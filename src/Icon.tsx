const paths: Record<string, string> = {
  Home: "m3 10 9-7 9 7v10a1 1 0 0 1-1 1h-5v-7H9v7H4a1 1 0 0 1-1-1Z",
  Search: "M21 21l-5-5 M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0",
  Compose: "M16 3l5 5-12 12-6 1 1-6Z M14 5l5 5",
  Notifications: "M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9 M10 21h4",
  Messages: "M21 11a9 9 0 0 1-9 9H3l2-5A9 9 0 1 1 21 11Z",
  Publishing: "M4 5h16v16H4Z M8 2v6 M16 2v6 M4 11h16",
  Settings:
    "M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8 M12 2v3 M12 19v3 M2 12h3 M19 12h3 M5 5l2 2 M17 17l2 2 M5 19l2-2 M17 7l2-2",
  Image: "M3 3h18v18H3Z M3 16l5-5 5 5 4-4 4 4 M16 7h.01",
  Smile:
    "M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0 M8 14q4 5 8 0 M8 8h.01 M16 8h.01",
  Link: "M10 13a5 5 0 0 0 7 0l3-3a5 5 0 0 0-7-7l-2 2 M14 11a5 5 0 0 0-7 0l-3 3a5 5 0 0 0 7 7l2-2",
  Heart: "M12 21 3 12C-3 4 6-2 12 5 18-2 27 4 21 12Z",
  Reply: "M9 10H3l6-6 M3 10h10q8 0 8 10",
  Repost: "M4 8h14l-4-4 M20 16H6l4 4 M18 8v5 M6 16v-5",
  Bookmark: "M6 3h12v19l-6-5-6 5Z",
  More: "M5 12h.01 M12 12h.01 M19 12h.01",
  Sun: "M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8 M12 2v2 M12 20v2 M2 12h2 M20 12h2 M4 4l2 2 M18 18l2 2 M4 20l2-2 M18 6l2-2",
  Plus: "M12 5v14 M5 12h14",
  Arrow: "M5 12h14 M14 7l5 5-5 5",
  Globe:
    "M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0 M3 12h18 M12 3q8 9 0 18-8-9 0-18",
};
export function Icon({ name }: { name: string }) {
  return (
    <svg className="icon" viewBox="0 0 24 24" aria-hidden="true">
      <path d={paths[name] || paths.Globe} />
    </svg>
  );
}
