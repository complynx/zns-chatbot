"""Run the shared counted fixture with mock-only TLS and bounded termination."""
import argparse
import os
import signal
import ssl

from counted_provider import FixtureServer, Handler


class TLSHandler(Handler):
    def setup(self):
        # Handshake runs in the daemon request thread, after accept sets a timeout.
        self.request.do_handshake()
        super().setup()


def terminate(_signum, _frame):
    # Pending responses stay uncertain; daemon HTTP threads must not delay exit.
    raise SystemExit(0)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mailbox", required=True)
    parser.add_argument("--listen", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=18089)
    parser.add_argument("--tls-cert")
    parser.add_argument("--tls-key")
    args = parser.parse_args()
    if os.environ.get("ZINUSYA_SYNTHETIC_COUNTED_QA") != "1":
        parser.error("Explicit synthetic fixture opt-in required; never production")
    if bool(args.tls_cert) != bool(args.tls_key):
        parser.error("Both TLS certificate and key are required")
    if not args.tls_cert and args.listen not in ("127.0.0.1", "::1", "localhost"):
        parser.error("TLS required outside loopback")
    signal.signal(signal.SIGTERM, terminate)
    with FixtureServer((args.listen, args.port), args.mailbox,
                       os.environ.get("ZINUSYA_COUNTED_QA_KEY", "")) as server:
        if args.tls_cert:
            context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            context.load_cert_chain(args.tls_cert, args.tls_key)
            server.RequestHandlerClass = TLSHandler
            server.socket = context.wrap_socket(server.socket, server_side=True,
                                                do_handshake_on_connect=False)
        print("Synthetic counted mock fixture ready", flush=True)
        server.serve_forever()


if __name__ == "__main__":
    main()
