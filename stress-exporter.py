from http.server import BaseHTTPRequestHandler, HTTPServer
import os
import time

SERIES_COUNT = int(os.getenv("SERIES_COUNT", "10000"))
PORT = int(os.getenv("PORT", "9183"))


class MetricsHandler(BaseHTTPRequestHandler):

    def do_GET(self):
        if self.path != "/metrics":
            self.send_response(404)
            self.end_headers()
            return

        # Changes on every scrape
        generation = time.time_ns()

        lines = [
            "# HELP prombridge_stress_value Synthetic metric for PromBridge load testing",
            "# TYPE prombridge_stress_value gauge",
        ]

        for i in range(SERIES_COUNT):
            value = generation + i
            lines.append(
                f'prombridge_stress_value{{series="{i:05d}"}} {value}'
            )

        body = ("\n".join(lines) + "\n").encode()

        self.send_response(200)
        self.send_header(
            "Content-Type",
            "text/plain; version=0.0.4; charset=utf-8"
        )
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()

        self.wfile.write(body)

    def log_message(self, format, *args):
        pass


print(
    f"PromBridge stress exporter listening on :{PORT}, "
    f"series={SERIES_COUNT}",
    flush=True
)

HTTPServer(("0.0.0.0", PORT), MetricsHandler).serve_forever()