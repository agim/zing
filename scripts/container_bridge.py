"""Loopback-only test ingress to a Docker internal network (no egress gateway)."""

import selectors
import socket
import threading


class Bridge:
    def __init__(self, port, destination):
        self.destination = destination
        self.closed = threading.Event()
        self.slots = threading.BoundedSemaphore(64)
        self.listener = socket.socket()
        self.listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self.listener.bind(("127.0.0.1", port))
        self.listener.listen(32)
        self.listener.settimeout(0.5)
        self.thread = threading.Thread(target=self.accept, daemon=True)
        self.thread.start()

    def accept(self):
        while not self.closed.is_set():
            try:
                client, _ = self.listener.accept()
            except socket.timeout:
                continue
            except OSError:
                return
            if not self.slots.acquire(blocking=False):
                client.close()
                continue
            threading.Thread(target=self.forward, args=(client,), daemon=True).start()

    def forward(self, client):
        try:
            # Resolve again on each connection, including after a fresh restore.
            with client, socket.create_connection(
                self.destination(), timeout=65
            ) as upstream, selectors.DefaultSelector() as selector:
                client.settimeout(65)
                selector.register(client, selectors.EVENT_READ, upstream)
                selector.register(upstream, selectors.EVENT_READ, client)
                while not self.closed.is_set():
                    ready = selector.select(timeout=65)
                    if not ready:
                        return
                    for key, _ in ready:
                        data = key.fileobj.recv(65536)
                        if not data:
                            return
                        key.data.sendall(data)
        except (OSError, RuntimeError, StopIteration):
            pass  # Fixture readiness polling reports unavailable upstreams.
        finally:
            client.close()
            self.slots.release()

    def close(self):
        self.closed.set()
        self.listener.close()
        self.thread.join(timeout=2)
