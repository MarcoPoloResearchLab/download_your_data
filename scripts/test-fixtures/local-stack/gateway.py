#!/usr/bin/env python3
import http.client
import http.server
import sys
import urllib.parse


listen_host, listen_port_text = sys.argv[1].rsplit(":", 1)
application_origin = urllib.parse.urlparse(sys.argv[2])


class GatewayHandler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/auth/session":
            self.send_response(http.client.NO_CONTENT)
            self.end_headers()
            return
        if self.path in {"/me", "/api/me"}:
            self.send_response(http.client.UNAUTHORIZED)
            self.end_headers()
            return

        connection = http.client.HTTPConnection(
            application_origin.hostname,
            application_origin.port,
            timeout=2,
        )
        try:
            connection.request("GET", self.path)
            response = connection.getresponse()
            body = response.read()
            self.send_response(response.status)
            for name, value in response.getheaders():
                if name.lower() not in {
                    "connection",
                    "content-length",
                    "transfer-encoding",
                }:
                    self.send_header(name, value)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        finally:
            connection.close()

    def log_message(self, _format, *_arguments):
        return


server = http.server.ThreadingHTTPServer(
    (listen_host, int(listen_port_text)),
    GatewayHandler,
)
server.serve_forever()
