#!/usr/bin/env python3
"""Loopback-only SMTP inbox for Zing development. Never forwards email."""
import argparse
import asyncio
import email.policy
import email.parser
import html
import json
import os
import re
from pathlib import Path
import threading
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MAX_MESSAGE = 12 * 1024 * 1024


def messages(directory):
    result = []
    for path in sorted(directory.glob("*.eml"), reverse=True)[:50]:
        message = email.parser.BytesParser(policy=email.policy.default).parsebytes(
            path.read_bytes()
        )
        body = (
            message.get_body(preferencelist=("plain",))
            if message.is_multipart()
            else message
        )
        text = body.get_content() if body else "(No plain-text body)"
        if not isinstance(text, str):
            text = text.decode("utf-8", errors="replace")
        result.append(
            {
                "id": path.stem,
                "from": str(message.get("From", "")),
                "to": str(message.get("To", "")),
                "subject": str(message.get("Subject", "")),
                "text": text[:1048576],
            }
        )
    return result


def rendered_text(text):
    escaped = html.escape(text)
    return re.sub(
        r"http://127\.0\.0\.1:3000/(?:verify|reset)\?token=[A-Za-z0-9_-]+",
        lambda match: '<a href="' + match[0] + '">' + match[0] + "</a>",
        escaped,
    )


class Inbox(BaseHTTPRequestHandler):
    directory = Path(".")

    def log_message(self, *_args):
        pass  # Do not put message bodies, addresses or tokens in logs.

    def do_GET(self):
        if self.path == "/health":
            payload, content_type = b"ok", "text/plain"
        elif self.path == "/messages":
            payload, content_type = (
                json.dumps(messages(self.directory)).encode(),
                "application/json",
            )
        elif self.path == "/":
            cards = "".join(
                "<article><h2>"
                + html.escape(m["subject"])
                + "</h2><p>"
                + html.escape(m["to"])
                + "</p><pre>"
                + rendered_text(m["text"])
                + "</pre></article>"
                for m in messages(self.directory)
            )
            markup = """<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Zing · Local inbox</title><style>body{font:15px system-ui;background:#f7f8fa;color:#15171b;max-width:900px;margin:40px auto;padding:0 20px}h1{color:#0085f3}article{background:white;border:1px solid #e9ebef;padding:22px;border-radius:14px;margin:16px 0}pre{white-space:pre-wrap;overflow-wrap:anywhere;font:14px/1.7 ui-monospace,monospace}p{color:#626773}a{color:#006ac4}</style><h1>Zing local inbox</h1><p>Development mail stays on this machine. Nothing is forwarded to the internet.</p><p><a href="/">Refresh inbox</a> · <a href="http://127.0.0.1:3000/">Open Zing</a></p>"""
            payload, content_type = (
                markup + (cards or "<article>No messages yet.</article>")
            ).encode(), "text/html; charset=utf-8"
        else:
            self.send_error(404)
            return
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header(
            "Content-Security-Policy",
            "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'",
        )
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)


async def smtp(reader, writer, directory):
    sender = False
    recipient = False

    async def send(line):
        writer.write((line + "\r\n").encode())
        await writer.drain()

    try:
        await send("220 localhost Zing development inbox")
        while True:
            raw = await asyncio.wait_for(reader.readline(), timeout=60)
            if not raw:
                break
            line = raw.decode("utf-8", errors="replace").strip()
            verb = line.split(" ", 1)[0].upper()
            if verb in ("EHLO", "HELO"):
                await send("250-localhost\r\n250 SIZE " + str(MAX_MESSAGE))
            elif verb == "MAIL":
                sender, recipient = True, False
                await send("250 OK")
            elif verb == "RCPT" and sender:
                recipient = True
                await send("250 OK")
            elif verb == "DATA" and recipient:
                await send("354 End with a dot on its own line")
                data = bytearray()
                while True:
                    chunk = await asyncio.wait_for(reader.readline(), timeout=60)
                    if not chunk:
                        raise ConnectionError("incomplete message")
                    if chunk in (b".\r\n", b".\n"):
                        break
                    data.extend(chunk[1:] if chunk.startswith(b"..") else chunk)
                    if len(data) > MAX_MESSAGE:
                        await send("552 Message too large")
                        return
                path = directory / (
                    str(time.time_ns()) + "-" + uuid.uuid4().hex + ".eml"
                )
                fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
                with os.fdopen(fd, "wb") as file:
                    file.write(data)
                sender, recipient = False, False
                await send("250 Stored locally")
            elif verb == "RSET":
                sender, recipient = False, False
                await send("250 OK")
            elif verb == "NOOP":
                await send("250 OK")
            elif verb == "QUIT":
                await send("221 Bye")
                break
            else:
                await send("503 Command not available")
    except (asyncio.TimeoutError, ConnectionError, ValueError):
        pass
    finally:
        writer.close()
        await writer.wait_closed()


async def main(args):
    directory = Path(args.directory)
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    Inbox.directory = directory
    server = ThreadingHTTPServer((args.bind, args.http_port), Inbox)
    smtp_server = await asyncio.start_server(
        lambda r, w: smtp(r, w, directory), args.bind, args.smtp_port
    )
    threading.Thread(target=server.serve_forever, daemon=True).start()
    print(
        f"Local inbox: http://127.0.0.1:{args.http_port} · SMTP: 127.0.0.1:{args.smtp_port}",
        flush=True,
    )
    try:
        async with smtp_server:
            await smtp_server.serve_forever()
    finally:
        server.shutdown()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--bind",
        default="127.0.0.1",
        help="Use 0.0.0.0 only inside an isolated container network",
    )
    parser.add_argument("--directory", default=".lidza/local-mail")
    parser.add_argument("--smtp-port", type=int, default=1025)
    parser.add_argument("--http-port", type=int, default=8025)
    try:
        asyncio.run(main(parser.parse_args()))
    except KeyboardInterrupt:
        pass
