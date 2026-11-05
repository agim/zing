package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/storage"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"io"
	"net/http"
	"strings"
	"zing/internal/social"
	"zing/packs/media_guard"
	"zing/schema"
)

func UploadMedia(ctx context.Context, req *router.Request[router.File]) (schema.MediaView, error) {
	out := schema.MediaView{}
	data, e := io.ReadAll(io.LimitReader(req.Body.Body, (8<<20)+1))
	if e != nil {
		return out, e
	}
	if len(data) > 8<<20 {
		return out, router.Errorf(413, "Image must be at most 8 MiB")
	}
	content := http.DetectContentType(data)
	if content != "image/png" && content != "image/jpeg" && content != "image/webp" && content != "image/gif" {
		return out, router.Errorf(422, "Upload a PNG, JPEG, WebP, or GIF")
	}
	normalized, err := media_guard.From(ctx).SanitizeImage(ctx, schema.ImageGuardInput{Data: base64.StdEncoding.EncodeToString(data)})
	if err != nil {
		return out, router.Errorf(422, "Image could not be decoded safely. Use a valid still PNG, JPEG, WebP or GIF within the image limits.")
	}
	data, e = base64.StdEncoding.DecodeString(normalized.Data)
	if e != nil {
		return out, e
	}
	content = normalized.Mime
	id := uuid.NewString()
	key := "users/" + social.Owner(ctx) + "/media/" + id
	_, e = storage.From(ctx).Put(ctx, key, bytes.NewReader(data), storage.PutOptions{ContentType: content})
	if e != nil {
		return out, e
	}
	_, e = db.From(ctx).Exec(ctx, `INSERT INTO media(id,owner_id,key,content_type) VALUES($1,$2,$3,$4)`, id, social.Owner(ctx), key, content)
	if e != nil {
		if cleanupErr := storage.From(ctx).Delete(ctx, key); cleanupErr != nil {
			return out, cleanupErr
		}
		return out, e
	}
	return schema.MediaView{ID: id, URL: "/api/v1/media/" + id}, nil
}
func MediaAlt(ctx context.Context, req *router.Request[schema.MediaAlt]) (schema.OperationResult, error) {
	tag, e := db.From(ctx).Exec(ctx, `UPDATE media SET alt=$3 WHERE owner_id=$1 AND id::text=$2`, social.Owner(ctx), req.Param("id"), strings.TrimSpace(req.Body.Alt))
	if e != nil {
		return schema.OperationResult{}, e
	}
	if tag.RowsAffected() == 0 {
		return schema.OperationResult{}, router.NotFound("Media")
	}
	return schema.OperationResult{Ok: true}, nil
}
func DownloadMedia(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var key string
	e := db.From(ctx).QueryRow(ctx, `SELECT key FROM media WHERE owner_id=$1 AND id::text=$2`, social.Owner(ctx), r.PathValue("id")).Scan(&key)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	rc, obj, e := storage.From(ctx).Get(ctx, key)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, e = io.Copy(w, rc); e != nil {
		return
	}
}
