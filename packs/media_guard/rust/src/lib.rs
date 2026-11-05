//! Uploaded files are decoded without filesystem/network access, under the
//! host's WASM memory/deadline limits. Only canonical still PNG leaves the pack.
pub mod abi;
pub mod schema;

use base64::{Engine, engine::general_purpose::STANDARD};
use image::{AnimationDecoder, DynamicImage, ImageDecoder, ImageFormat, ImageReader, Limits};
use schema::{ImageGuardInput, ImageGuardOutput};
use std::io::Cursor;

fn sanitize(input: ImageGuardInput) -> Result<ImageGuardOutput, String> {
    if input.data.len() > 11_184_812 {
        return Err("Image must be at most 8 MiB".into());
    }
    let bytes = STANDARD
        .decode(input.data)
        .map_err(|_| "Invalid image encoding")?;
    if bytes.is_empty() || bytes.len() > 8 * 1024 * 1024 {
        return Err("Image must be at most 8 MiB".into());
    }
    let format =
        image::guess_format(&bytes).map_err(|_| "Upload a PNG, JPEG, WebP or still GIF")?;
    if !matches!(
        format,
        ImageFormat::Png | ImageFormat::Jpeg | ImageFormat::WebP | ImageFormat::Gif
    ) {
        return Err("Upload a PNG, JPEG, WebP or still GIF".into());
    }
    let (width, height) = ImageReader::with_format(Cursor::new(&bytes), format)
        .into_dimensions()
        .map_err(|_| "Image headers are damaged")?;
    if width == 0
        || height == 0
        || width > 8192
        || height > 8192
        || (width as u64) * (height as u64) > 16_000_000
    {
        return Err("Image exceeds 16 million pixels or 8192 pixels on one side".into());
    }
    let mut limits = Limits::default();
    limits.max_image_width = Some(8192);
    limits.max_image_height = Some(8192);
    limits.max_alloc = Some(96 * 1024 * 1024);
    if format == ImageFormat::Png
        && image::codecs::png::PngDecoder::new(Cursor::new(&bytes))
            .map_err(|_| "PNG is damaged")?
            .is_apng()
            .map_err(|_| "PNG animation metadata is damaged")?
    {
        return Err("Choose a still image; animated uploads are not supported".into());
    }
    let image = if format == ImageFormat::Gif {
        let mut decoder = image::codecs::gif::GifDecoder::new(Cursor::new(&bytes))
            .map_err(|_| "GIF is damaged")?;
        decoder
            .set_limits(limits)
            .map_err(|_| "Image exceeds decoder limits")?;
        let mut frames = decoder.into_frames();
        let frame = frames
            .next()
            .ok_or("GIF has no image")?
            .map_err(|_| "GIF is damaged")?;
        if frames.next().is_some() {
            return Err("Choose a still image; animated uploads are not supported".into());
        }
        DynamicImage::ImageRgba8(frame.into_buffer())
    } else {
        if format == ImageFormat::WebP {
            let decoder = image::codecs::webp::WebPDecoder::new(Cursor::new(&bytes))
                .map_err(|_| "WebP is damaged")?;
            if decoder.has_animation() {
                return Err("Choose a still image; animated uploads are not supported".into());
            }
        }
        let mut reader = ImageReader::with_format(Cursor::new(&bytes), format);
        reader.limits(limits);
        let mut decoder = reader.into_decoder().map_err(|_| "Image data is damaged")?;
        let orientation = decoder
            .orientation()
            .map_err(|_| "Image orientation metadata is damaged")?;
        let mut decoded = DynamicImage::from_decoder(decoder)
            .map_err(|_| "Image data is damaged or exceeds decoder limits")?;
        decoded.apply_orientation(orientation);
        decoded
    };
    let image = DynamicImage::ImageRgba8(image.thumbnail(1400, 1400).to_rgba8());
    let mut output = Cursor::new(Vec::new());
    image
        .write_to(&mut output, ImageFormat::Png)
        .map_err(|_| "Image could not be normalized")?;
    if output.get_ref().len() > 8 * 1024 * 1024 {
        return Err("Normalized image exceeds 8 MiB".into());
    }
    Ok(ImageGuardOutput {
        data: STANDARD.encode(output.into_inner()),
        mime: "image/png".into(),
        width: image.width() as i32,
        height: image.height() as i32,
    })
}
lidza_export!(
    sanitize_image,
    |input: ImageGuardInput| -> Result<ImageGuardOutput, String> { sanitize(input) }
);

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn rejects_header_only_and_unknown_payloads() {
        for bytes in [
            b"\x89PNG\r\n\x1a\n".as_slice(),
            b"<script>bad()</script>".as_slice(),
        ] {
            assert!(
                sanitize(ImageGuardInput {
                    data: STANDARD.encode(bytes)
                })
                .is_err()
            );
        }
    }
    #[test]
    fn rejects_animation_and_oversized_dimensions() {
        let mut bytes = Cursor::new(Vec::new());
        {
            let mut encoder = image::codecs::gif::GifEncoder::new(&mut bytes);
            let frames = (0..2).map(|_| image::Frame::new(image::RgbaImage::new(2, 2)));
            encoder.encode_frames(frames).unwrap();
        }
        assert!(
            sanitize(ImageGuardInput {
                data: STANDARD.encode(bytes.into_inner())
            })
            .is_err()
        );
        let mut wide = Cursor::new(Vec::new());
        DynamicImage::new_rgb8(8193, 1)
            .write_to(&mut wide, ImageFormat::Png)
            .unwrap();
        assert!(
            sanitize(ImageGuardInput {
                data: STANDARD.encode(wide.into_inner())
            })
            .is_err()
        );
    }
    #[test]
    fn creates_canonical_png_and_bounds_dimensions() {
        let image = DynamicImage::new_rgb8(1600, 20);
        let mut bytes = Cursor::new(Vec::new());
        image.write_to(&mut bytes, ImageFormat::Png).unwrap();
        let result = sanitize(ImageGuardInput {
            data: STANDARD.encode(bytes.into_inner()),
        })
        .unwrap();
        assert!(result.width <= 1400);
        assert_eq!(result.mime, "image/png");
        assert!(image::load_from_memory(&STANDARD.decode(result.data).unwrap()).is_ok());
    }
}
