#!/usr/bin/python3
"""fnOS desktop CGI stub: reverse-proxy to the wxbackup HTTP server.

Strips /cgi/ThirdParty/wxbackup/index.cgi and forwards the remainder to
http://127.0.0.1:20365 (WXBACKUP_PORT / TRIM_SERVICE_PORT override).
"""
from __future__ import print_function

import os
import sys

try:
    from http.client import HTTPConnection
except ImportError:  # pragma: no cover
    from httplib import HTTPConnection  # type: ignore

HOP_BY_HOP = {
    "connection",
    "keep-alive",
    "proxy-authenticate",
    "proxy-authorization",
    "te",
    "trailers",
    "transfer-encoding",
    "upgrade",
    "host",
    "content-length",
}

CGI_PREFIX = "/cgi/ThirdParty/wxbackup/index.cgi"


def backend_addr():
    port = (
        os.environ.get("WXBACKUP_PORT")
        or os.environ.get("TRIM_SERVICE_PORT")
        or "20365"
    )
    return "127.0.0.1", int(port)


def target_path():
    uri = os.environ.get("REQUEST_URI") or ""
    path = os.environ.get("PATH_INFO") or ""
    query = os.environ.get("QUERY_STRING") or ""
    if CGI_PREFIX in uri:
        rest = uri.split(CGI_PREFIX, 1)[1]
        if "?" in rest:
            path, query = rest.split("?", 1)
        else:
            path = rest
            if not query and "?" in uri:
                query = uri.split("?", 1)[1]
    if not path:
        path = "/"
    if not path.startswith("/"):
        path = "/" + path
    if query:
        return path + "?" + query
    return path


def request_headers():
    headers = {}
    for key, value in os.environ.items():
        if not key.startswith("HTTP_"):
            continue
        name = key[5:].replace("_", "-").title()
        if name.lower() in HOP_BY_HOP:
            continue
        headers[name] = value
    ctype = os.environ.get("CONTENT_TYPE")
    if ctype:
        headers["Content-Type"] = ctype
    return headers


def request_body():
    length = os.environ.get("CONTENT_LENGTH") or "0"
    try:
        n = int(length)
    except ValueError:
        n = 0
    if n <= 0:
        return None
    return sys.stdin.buffer.read(n)


def cgi_status_line(status):
    if not status:
        return "200 OK"
    text = str(status)
    if text[0].isdigit() and " " not in text:
        return text + " " + {
            200: "OK",
            201: "Created",
            204: "No Content",
            301: "Moved Permanently",
            302: "Found",
            304: "Not Modified",
            400: "Bad Request",
            401: "Unauthorized",
            403: "Forbidden",
            404: "Not Found",
            405: "Method Not Allowed",
            502: "Bad Gateway",
            503: "Service Unavailable",
        }.get(int(text), "OK")
    return text


def write_headers(status, headers):
    out = sys.stdout.buffer
    out.write(("Status: %s\r\n" % cgi_status_line(status)).encode("ascii", "replace"))
    for name, value in headers:
        if name.lower() in HOP_BY_HOP:
            continue
        line = "%s: %s\r\n" % (name, value)
        out.write(line.encode("latin-1", "replace"))
    out.write(b"\r\n")
    out.flush()


def fail(code, message):
    body = message.encode("utf-8")
    write_headers(code, [("Content-Type", "text/plain; charset=utf-8"), ("Content-Length", str(len(body)))])
    if os.environ.get("REQUEST_METHOD", "GET").upper() != "HEAD":
        sys.stdout.buffer.write(body)
        sys.stdout.buffer.flush()


def main():
    host, port = backend_addr()
    method = (os.environ.get("REQUEST_METHOD") or "GET").upper()
    path = target_path()
    try:
        conn = HTTPConnection(host, port, timeout=300)
        conn.request(method, path, body=request_body(), headers=request_headers())
        resp = conn.getresponse()
    except Exception as exc:
        fail(502, "wxbackup backend 127.0.0.1:%s unreachable: %s" % (port, exc))
        return
    write_headers(resp.status, resp.getheaders())
    if method == "HEAD":
        conn.close()
        return
    out = sys.stdout.buffer
    while True:
        chunk = resp.read(65536)
        if not chunk:
            break
        out.write(chunk)
        out.flush()
    conn.close()


if __name__ == "__main__":
    main()
