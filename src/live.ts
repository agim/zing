import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

// Events contain invalidation hints only. Bodies remain behind owner-scoped APIs.
// Catch up on every connection, retain cached values on failed reads, and cancel
// every timer/socket on account changes or an explicit pause.
export function useUserLive(owner: string | undefined, paused: boolean) {
  const client = useQueryClient();
  useEffect(() => {
    if (!owner || paused) return;
    let socket: WebSocket | undefined;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let stopped = false;
    let delay = 1000;
    const refresh = () => {
      for (const key of [
        "feed",
        "notifications",
        "messages",
        "conversations",
        "deliveries",
        "hosting",
      ]) {
        void client.invalidateQueries({ queryKey: [key] });
      }
    };
    const connect = () => {
      if (
        stopped ||
        socket?.readyState === WebSocket.OPEN ||
        socket?.readyState === WebSocket.CONNECTING
      )
        return;
      const scheme = location.protocol === "https:" ? "wss" : "ws";
      socket = new WebSocket(
        `${scheme}://${location.host}/api/v1/realtime?topics=${encodeURIComponent(`user:${owner}`)}`,
      );
      socket.onopen = () => {
        delay = 1000;
        refresh();
      };
      socket.onmessage = () => refresh();
      socket.onclose = () => {
        if (stopped) return;
        timer = setTimeout(connect, delay);
        delay = Math.min(delay * 2, 30000);
      };
      socket.onerror = () => {
        void client.invalidateQueries({ queryKey: ["session"] });
      };
    };
    const online = () => {
      if (timer) clearTimeout(timer);
      connect();
    };
    window.addEventListener("online", online);
    connect();
    return () => {
      stopped = true;
      if (timer) clearTimeout(timer);
      window.removeEventListener("online", online);
      socket?.close();
    };
  }, [client, owner, paused]);
}
