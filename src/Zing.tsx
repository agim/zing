import {
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useLocation, useNavigate } from "@tanstack/react-router";
import {
  api,
  ApiError,
  type SocialPost,
  type ConnectionView,
  type DraftView,
  type ConversationView,
  type ComposeRequest,
  type SocialAccount,
  type MessageView,
} from "@lidza/client";
import { Icon } from "./Icon";
import { useUserLive } from "./live";

type Page =
  | "Home"
  | "Search"
  | "Compose"
  | "Notifications"
  | "Messages"
  | "Publishing"
  | "Settings";
const pages: Page[] = [
  "Home",
  "Search",
  "Notifications",
  "Messages",
  "Publishing",
  "Settings",
];
const names: Record<string, string> = {
  bluesky: "Bluesky",
  mastodon: "Mastodon",
  threads: "Threads",
};
const blank = (): ComposeRequest => ({
  text: "",
  destinations: [],
  mediaIds: [],
  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
});
function message(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.fields)
      return e.fields.map((f) => `${f.field}: ${f.message}`).join("; ");
    const detail = (e.body as { error?: unknown } | undefined)?.error;
    return typeof detail === "string"
      ? detail
      : "Request failed. Please try again.";
  }
  return e instanceof Error
    ? e.message
    : "Something went wrong. Please try again.";
}
function safeURL(raw: string) {
  try {
    const u = new URL(raw);
    return u.protocol === "https:" ? u.href : undefined;
  } catch {
    return undefined;
  }
}
function Avatar({
  name,
  src,
  onClick,
}: {
  name: string;
  src?: string;
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      className="avatar"
      onClick={onClick}
      aria-label={onClick ? `View ${name}` : name}
    >
      {safeURL(src || "") ? (
        <img
          src={safeURL(src!)}
          alt=""
          referrerPolicy="no-referrer"
          className="avatar-image"
        />
      ) : (
        name.slice(0, 2).toUpperCase()
      )}
    </button>
  );
}
function Empty({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="card empty">
      <Icon name="Globe" />
      <h2>{title}</h2>
      {children}
    </div>
  );
}
function Dialog({
  title,
  children,
  onClose,
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    ref.current?.showModal();
  }, []);
  return (
    <dialog ref={ref} className="modal" onClose={onClose} aria-label={title}>
      <div className="row">
        <h2>{title}</h2>
        <button onClick={onClose} aria-label="Close dialog">
          ✕
        </button>
      </div>
      {children}
    </dialog>
  );
}
function Notices({ items }: { items?: string[] }) {
  return (
    <>
      {items?.map((v, i) => (
        <div className="notice warning" key={i} role="status">
          {v}
        </div>
      ))}
    </>
  );
}
function ErrorNotice({ error }: { error: unknown }) {
  return error ? (
    <div className="notice warning" role="alert">
      {message(error)}
    </div>
  ) : null;
}

export function Zing() {
  const queryClient = useQueryClient();
  const location = useLocation();
  const navigate = useNavigate();
  const section = location.hash.split("?")[0];
  const page: Page =
    pages.includes(section as Page) || section === "Compose"
      ? (section as Page)
      : "Home";
  const path = location.pathname;
  const [network, setNetwork] = useState(""),
    [search, setSearch] = useState(""),
    [searchInput, setSearchInput] = useState("");
  const [theme, setTheme] = useState("system"),
    [notice, setNotice] = useState(""),
    [failure, setFailure] = useState<unknown>(),
    [busy, setBusy] = useState(false);
  const [compose, setCompose] = useState<ComposeRequest>(blank),
    [draftId, setDraftId] = useState(""),
    [schedule, setSchedule] = useState("");
  const [connecting, setConnecting] = useState(""),
    [conversation, setConversation] = useState<ConversationView | null>(null),
    [profile, setProfile] = useState<
      (SocialAccount & { connectionId: string }) | null
    >(null),
    [postMenu, setPostMenu] = useState<SocialPost | null>(null);
  const [reporting, setReporting] = useState<{
    connectionId: string;
    target: string;
    kind: "post" | "account" | "message";
    messageId?: string;
  } | null>(null);
  const [reportReason, setReportReason] = useState("");
  const [attachEvidence, setAttachEvidence] = useState(false);
  const [appealReason, setAppealReason] = useState("");
  const [relayCandidate, setRelayCandidate] = useState("");
  const [publicationTab, setPublicationTab] = useState("Drafts"),
    [feedItems, setFeedItems] = useState<SocialPost[]>([]),
    [cursor, setCursor] = useState("");
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api.authSession(),
    retry: false,
  });
  const user = session.data?.user;
  const [livePaused, setLivePaused] = useState(false);
  useUserLive(user?.verified ? user.subject : undefined, livePaused);
  const pendingSends = useRef(new Map<string, { text: string; id: string }>());
  const [olderMessages, setOlderMessages] = useState<MessageView[]>([]);
  const [olderCursor, setOlderCursor] = useState<string | undefined>();
  const [feedScope, setFeedScope] = useState("following");

  const connections = useQuery({
    queryKey: ["connections", user?.subject],
    queryFn: () => api.connections(),
    enabled: !!user?.verified,
    retry: false,
  });
  const conns = connections.data?.items || [];
  const hosting = useQuery({
    queryKey: ["hosting", user?.subject],
    queryFn: () => api.hosting(),
    enabled: !!user?.verified,
    retry: false,
  });
  const [hostedNetwork, setHostedNetwork] = useState("mastodon");
  const [hostedUsername, setHostedUsername] = useState("");

  const feed = useQuery({
    queryKey: ["feed", user?.subject, network, search, feedScope],
    queryFn: () =>
      page === "Search"
        ? api.search({ query: { network, q: search, scope: feedScope } })
        : api.feed({ query: { network, scope: feedScope } }),
    enabled: !!user?.verified && (page === "Home" || page === "Search"),
    retry: false,
  });
  const notifications = useQuery({
    queryKey: ["notifications", user?.subject],
    queryFn: () => api.notifications(),
    enabled: !!user?.verified && page === "Notifications",
    retry: false,
  });
  const conversations = useQuery({
    queryKey: ["conversations", user?.subject],
    queryFn: () => api.conversations(),
    enabled: !!user?.verified && page === "Messages",
    retry: false,
  });
  const messages = useQuery({
    queryKey: [
      "messages",
      user?.subject,
      conversation?.connectionId,
      conversation?.id,
    ],
    queryFn: () =>
      api.messages({
        query: {
          connectionId: conversation!.connectionId,
          conversationId: conversation!.id,
        },
      }),
    enabled: !!user?.verified && !!conversation && page === "Messages",
    retry: false,
  });
  const drafts = useQuery({
    queryKey: ["drafts", user?.subject],
    queryFn: () => api.drafts(),
    enabled: !!user?.verified && page === "Publishing",
    retry: false,
  });
  const deliveries = useQuery({
    queryKey: ["deliveries", user?.subject],
    queryFn: () => api.deliveries(),
    enabled: !!user?.verified && page === "Publishing",
    retry: false,
  });
  useEffect(() => {
    setOlderMessages([]);
    setOlderCursor(undefined);
  }, [conversation?.id, conversation?.connectionId, user?.subject]);
  useEffect(() => {
    setFeedItems([]);
    setCursor("");
  }, [network, search, feedScope]);
  useEffect(() => {
    pendingSends.current.clear();
  }, [user?.subject]);
  useEffect(() => {
    if (window.location.hash.includes("connect_error="))
      setFailure(
        new Error("Account authorization failed or expired. Please reconnect."),
      );
    const t = localStorage.getItem("zing-theme") || "system";
    setTheme(t);
  }, []);
  useEffect(() => {
    if (theme === "system") delete document.body.dataset.theme;
    else document.body.dataset.theme = theme;
    localStorage.setItem("zing-theme", theme);
  }, [theme]);
  useEffect(() => {
    setFeedItems([]);
    setConversation(null);
    setProfile(null);
    setCompose(blank());
    setDraftId("");
    setSchedule("");
  }, [user?.subject]);
  const firstPageKeys = useRef(new Set<string>());
  useEffect(() => {
    firstPageKeys.current.clear();
  }, [network, search, feedScope, user?.subject]);
  useEffect(() => {
    if (feed.data) {
      const items = feed.data.items;
      const fresh = new Set(items.map((p) => p.connectionId + p.id));
      const previous = firstPageKeys.current;
      setFeedItems((old) => [
        ...items,
        ...old.filter(
          (p) =>
            !previous.has(p.connectionId + p.id) &&
            !fresh.has(p.connectionId + p.id),
        ),
      ]);
      firstPageKeys.current = fresh;
      setCursor(feed.data.cursor);
    }
  }, [feed.data]);
  useEffect(() => {
    if (!notice) return;
    const t = window.setTimeout(() => setNotice(""), 5000);
    return () => clearTimeout(t);
  }, [notice]);
  function go(p: Page) {
    setFailure(undefined);
    void navigate({ to: "/", hash: p });
  }
  async function act(fn: () => Promise<unknown>, success?: string) {
    if (busy) return;
    setBusy(true);
    setFailure(undefined);
    try {
      await fn();
      if (success) setNotice(success);
      return true;
    } catch (e) {
      setFailure(e);
      return false;
    } finally {
      setBusy(false);
    }
  }
  function refresh(key: string) {
    void queryClient.invalidateQueries({ queryKey: [key] });
  }
  async function save(publish = false) {
    const body = {
      ...compose,
      scheduledAt: schedule ? new Date(schedule).toISOString() : undefined,
    };
    const saved = draftId
      ? await api.updateDraft({ id: draftId }, body)
      : await api.saveDraft(body);
    setDraftId(saved.id);
    if (publish) {
      await api.publish({ id: saved.id });
      setCompose(blank());
      setDraftId("");
      setSchedule("");
      localStorage.removeItem("zing-unsaved:" + user?.subject);
      setPublicationTab("Delivery history");
      go("Publishing");
    } else {
      setPublicationTab("Drafts");
      go("Publishing");
    }
    refresh("drafts");
    refresh("deliveries");
    setNotice(
      publish
        ? "Queued. Follow delivery status for each destination."
        : "Draft saved.",
    );
  }
  async function action(p: SocialPost, action: string, target = p.id) {
    await api.action({
      connectionId: p.connectionId,
      action,
      target,
      cid: p.cid,
    });
    refresh("feed");
    setNotice("Action completed on " + names[p.network]);
  }
  async function openProfile(p: SocialPost) {
    const v = await api.profile({
      query: { connectionId: p.connectionId, id: p.author.id },
    });
    setProfile({ ...v, connectionId: p.connectionId });
  }
  const people = useQuery({
    queryKey: ["people", user?.subject, network, search],
    queryFn: () =>
      api.people({ query: { network, q: search, scope: feedScope } }),
    enabled: !!user?.verified && page === "Search" && !!search,
    retry: false,
  });
  const errors = [
    people.error,
    failure,
    connections.error,
    page === "Home" || page === "Search" ? feed.error : null,
    page === "Messages" ? messages.error || conversations.error : null,
    page === "Publishing" ? drafts.error || deliveries.error : null,
    page === "Notifications" ? notifications.error : null,
  ];
  if (path === "/privacy" || path === "/terms")
    return (
      <div className="auth card form-card">
        <Brand />
        <h1 className="title">
          {path === "/privacy" ? "Your privacy" : "Terms of use"}
        </h1>
        <p className="subtitle">
          Zing stores your account, encrypted network credentials, drafts,
          schedules, uploaded media, and delivery records to operate the
          service. Connected-network content remains subject to that network’s
          policies. Private messages stay on their original network. Account
          deletion removes Zing records and queues media removal; posts already
          published elsewhere remain there.
        </p>
        <p className="subtitle">
          Use only accounts you control and respect each network’s rules.
          Operator contact and final legal terms must be configured before
          public launch.
        </p>
        <a href="/">Back to Zing</a>
      </div>
    );
  if (session.isPending)
    return (
      <div className="auth">
        <Brand />
        <Empty title="Gathering your world…" />
      </div>
    );
  if (session.error)
    return (
      <div className="auth">
        <Brand />
        <ErrorNotice error={session.error} />
        <button className="primary" onClick={() => void session.refetch()}>
          Try again
        </button>
      </div>
    );
  if (
    !user ||
    ["/login", "/register", "/forgot", "/reset", "/verify"].includes(path)
  )
    return (
      <AuthPage
        path={path}
        onDone={() => {
          void navigate({ to: "/", hash: "Home" });
          refresh("session");
        }}
      />
    );
  if (!user.verified)
    return (
      <div className="auth card form-card">
        <Brand />
        <h1 className="title">Check your inbox.</h1>
        <p className="meta">
          Installing this server? <a href="/setup">Continue operator setup</a>
        </p>
        <p className="subtitle">
          Verify {user.email} before connecting social accounts.
        </p>
        <ErrorNotice error={failure} />
        <button
          disabled={busy}
          className="primary full"
          onClick={() =>
            void act(() => api.authVerifyResend(), "Verification email queued.")
          }
        >
          Resend verification email
        </button>
        <button
          className="secondary full"
          onClick={() => void session.refetch()}
        >
          I’ve verified my email
        </button>
        <p>
          <a href="/verify">Enter verification token</a>
        </p>
        <button
          onClick={() =>
            void act(async () => {
              await api.authLogout();
              refresh("session");
            })
          }
        >
          Sign out
        </button>
        {notice && <p role="status">{notice}</p>}
      </div>
    );
  const displayName = user.name || user.email?.split("@")[0] || "friend";
  return (
    <>
      <div className="app">
        <aside className="sidebar">
          <Brand />
          <nav className="nav" aria-label="Main navigation">
            {pages.map((n) => (
              <button
                key={n}
                data-page={n}
                onClick={() => go(n)}
                className={page === n ? "active" : ""}
              >
                <Icon name={n} />
                <span>{n}</span>
              </button>
            ))}
          </nav>
          <button className="primary" onClick={() => go("Compose")}>
            <Icon name="Compose" />
            Create a post
          </button>
          <div className="sidebar-bottom">
            <div className="prototype">YOUR WORLD, CONNECTED</div>
            <div className="account">
              <Avatar name={displayName} />
              <div>
                <div className="name">{displayName}</div>
                <div className="handle">{conns.length} connected accounts</div>
              </div>
              <button aria-label="Settings" onClick={() => go("Settings")}>
                <Icon name="More" />
              </button>
            </div>
          </div>
        </aside>
        <main className="main">
          <header className="topbar">
            <div>
              <div className="eyebrow">YOUR WORLD, CONNECTED</div>
              <div className="greeting">
                {page === "Home" ? `Good to see you, ${displayName}.` : page}
              </div>
            </div>
            <div className="top-actions">
              <button
                className="round"
                aria-label="Change theme"
                onClick={() =>
                  setTheme(
                    theme === "system"
                      ? "light"
                      : theme === "light"
                        ? "dark"
                        : "system",
                  )
                }
              >
                <Icon name="Sun" />
              </button>
              <Avatar name={displayName} onClick={() => go("Settings")} />
            </div>
          </header>
          {errors.filter(Boolean).map((e, i) => (
            <ErrorNotice error={e} key={i} />
          ))}
          <button
            className="small secondary"
            onClick={() => setLivePaused((value) => !value)}
            aria-pressed={livePaused}
          >
            {livePaused ? "Resume live updates" : "Pause live updates"}
          </button>
          {(page === "Home" || page === "Search") && (
            <>
              {page === "Home" ? (
                <>
                  <div className="hero-note">
                    <span className="status-ring" />A little from everywhere.
                    All in one place.
                  </div>
                  <div className="card composer">
                    <div className="composer-row">
                      <Avatar name={displayName} />
                      <button
                        className="compose-prompt"
                        onClick={() => go("Compose")}
                      >
                        What’s on your mind, {displayName}?
                      </button>
                    </div>
                    <div className="composer-footer">
                      <span className="meta">
                        Publish to the accounts you choose
                      </span>
                      <button className="primary" onClick={() => go("Compose")}>
                        Create a post <Icon name="Arrow" />
                      </button>
                    </div>
                  </div>
                </>
              ) : (
                <>
                  <h1 className="title">Find your people.</h1>
                  <p className="subtitle">
                    Search posts across your connected networks.
                  </p>
                  <form
                    className="searchbox"
                    onSubmit={(e) => {
                      e.preventDefault();
                      setSearch(searchInput);
                    }}
                  >
                    <Icon name="Search" />
                    <input
                      aria-label="Search posts"
                      value={searchInput}
                      onChange={(e) => setSearchInput(e.target.value)}
                      placeholder="Search the connected world"
                    />
                    <button className="primary">Search</button>
                  </form>
                </>
              )}
              <div className="tabs">
                {["", ...Object.keys(names)].map((n) => (
                  <button
                    key={n}
                    className={network === n ? "active" : ""}
                    onClick={() => {
                      setNetwork(n);
                      setFeedItems([]);
                    }}
                  >
                    {n ? names[n] : "All networks"}
                  </button>
                ))}
                <button
                  className="filter"
                  aria-label="Refresh feed"
                  onClick={() => void feed.refetch()}
                >
                  <Icon name="Repost" />
                </button>
              </div>
              {page === "Search" && !!search && (
                <>
                  <h2 className="section-title">People</h2>
                  <ErrorNotice error={people.error} />
                  <Notices items={people.data?.warnings} />
                  {people.data?.items.map((v) => (
                    <div
                      key={v.connectionId + v.profile.id}
                      className="card form-card"
                    >
                      <div className="row">
                        <Avatar
                          name={v.profile.name || v.profile.handle}
                          src={v.profile.avatar}
                          onClick={() =>
                            setProfile({
                              ...v.profile,
                              connectionId: v.connectionId,
                            })
                          }
                        />
                        <button
                          className="name"
                          onClick={() =>
                            setProfile({
                              ...v.profile,
                              connectionId: v.connectionId,
                            })
                          }
                        >
                          {v.profile.name || v.profile.handle}
                        </button>
                        <span className={"network " + v.network}>
                          {names[v.network]}
                        </span>
                      </div>
                      <p className="meta">@{v.profile.handle}</p>
                      <p className="post-body">{v.profile.bio}</p>
                    </div>
                  ))}
                  <h2 className="section-title">Posts</h2>
                </>
              )}
              {conns.some((c) => c.hosted && c.network === "bluesky") && (
                <label className="field">
                  Hosted Bluesky feed
                  <select
                    aria-label="Hosted Bluesky feed"
                    value={feedScope}
                    onChange={(event) => setFeedScope(event.target.value)}
                  >
                    <option value="following">Following</option>
                    <option value="discover">Discover local posts</option>
                  </select>
                </label>
              )}
              <Notices items={feed.data?.warnings} />
              {feed.isPending && (
                <div role="status" className="notice">
                  Gathering your worlds…
                </div>
              )}
              {!feed.isPending && !feedItems.length && (
                <Empty
                  title={
                    conns.length
                      ? "No posts here yet."
                      : "Your world starts here."
                  }
                >
                  <p>
                    {conns.length
                      ? "Try another network or search."
                      : hosting.data?.networks.some((n) => n.available)
                        ? hosting.data?.enrollment?.message ||
                          "Create a hosted identity and start posting on your own server."
                        : "Connect your accounts and bring your conversations together."}
                  </p>
                  <button className="primary" onClick={() => go("Settings")}>
                    {hosting.data?.networks.some((n) => n.available)
                      ? hosting.data?.enrollment
                        ? "View your social identities"
                        : "Create your hosted identity"
                      : "Connect accounts"}
                  </button>
                </Empty>
              )}
              {feedItems.map((p) => (
                <article className="card post" key={p.connectionId + p.id}>
                  <div className="post-head">
                    <Avatar
                      name={p.author.name || p.author.handle}
                      src={p.author.avatar}
                      onClick={() => void act(() => openProfile(p))}
                    />
                    <div>
                      <div className="post-name">
                        <button
                          className="name"
                          onClick={() => void act(() => openProfile(p))}
                        >
                          {p.author.name || p.author.handle}
                        </button>
                        <span className={"network " + p.network}>
                          {names[p.network]}
                        </span>
                      </div>
                      <div className="handle">
                        @{p.author.handle} ·{" "}
                        {new Date(p.createdAt).toLocaleString()}
                      </div>
                    </div>
                    <button
                      className="more"
                      aria-label="Post options"
                      onClick={() => setPostMenu(p)}
                    >
                      <Icon name="More" />
                    </button>
                  </div>
                  <p className="post-body preserve">{p.text}</p>
                  {p.images
                    .filter((u) => safeURL(u))
                    .map((u, i) => (
                      <img
                        className="post-image"
                        key={u}
                        src={u}
                        alt={`Attachment ${i + 1} from ${p.author.handle}`}
                        loading="lazy"
                        referrerPolicy="no-referrer"
                      />
                    ))}
                  <div className="post-actions">
                    <button
                      disabled={
                        busy ||
                        !conns
                          .find((c) => c.id === p.connectionId)
                          ?.capabilities.includes("reply")
                      }
                      aria-label="Reply"
                      onClick={() => {
                        setCompose({
                          ...blank(),
                          destinations: [p.connectionId],
                          replyConnectionId: p.connectionId,
                          replyId: p.id,
                          relayParentDraftId: undefined,
                          replyCid: p.cid,
                          rootId: p.rootId || p.id,
                          rootCid: p.rootCid || p.cid,
                        });
                        setRelayCandidate(p.relayParentDraftId || "");
                        setDraftId("");
                        go("Compose");
                      }}
                    >
                      <Icon name="Messages" />
                      {p.replies}
                    </button>
                    <button
                      disabled={
                        busy ||
                        !conns
                          .find((c) => c.id === p.connectionId)
                          ?.capabilities.includes("repost")
                      }
                      aria-label="Repost"
                      onClick={() =>
                        void act(() =>
                          action(p, p.reposted ? "unrepost" : "repost"),
                        )
                      }
                    >
                      <Icon name="Repost" />
                      {p.reposts}
                    </button>
                    <button
                      className={p.liked ? "liked" : ""}
                      disabled={
                        busy ||
                        !conns
                          .find((c) => c.id === p.connectionId)
                          ?.capabilities.includes("like")
                      }
                      aria-label={p.liked ? "Unlike" : "Like"}
                      onClick={() =>
                        void act(() => action(p, p.liked ? "unlike" : "like"))
                      }
                    >
                      <Icon name="Heart" />
                      {p.likes}
                    </button>
                    {safeURL(p.url) && (
                      <a
                        href={safeURL(p.url)}
                        target="_blank"
                        rel="noreferrer"
                        aria-label="Open original post"
                      >
                        <Icon name="Link" />
                      </a>
                    )}
                  </div>
                </article>
              ))}
              {cursor && (
                <button
                  className="secondary full"
                  disabled={busy}
                  onClick={() =>
                    void act(async () => {
                      const more =
                        page === "Search"
                          ? await api.search({
                              query: {
                                network,
                                q: search,
                                cursor,
                                scope: feedScope,
                              },
                            })
                          : await api.feed({
                              query: { network, cursor, scope: feedScope },
                            });
                      setFeedItems((old) => {
                        const ids = new Set(
                          old.map((p) => p.connectionId + p.id),
                        );
                        return [
                          ...old,
                          ...more.items.filter(
                            (p) => !ids.has(p.connectionId + p.id),
                          ),
                        ];
                      });
                      setCursor(more.cursor);
                    })
                  }
                >
                  Load more
                </button>
              )}
            </>
          )}
          {page === "Compose" && (
            <>
              <h1 className="title">A little spark.</h1>
              <p className="subtitle">One thought. All your worlds.</p>
              <form
                className="card composer"
                onSubmit={(e) => {
                  e.preventDefault();
                  void act(() => save(true));
                }}
              >
                <div className="composer-row">
                  <Avatar name={displayName} />
                  <textarea
                    aria-label="Post text"
                    value={compose.text}
                    onChange={(e) =>
                      setCompose({ ...compose, text: e.target.value })
                    }
                    placeholder="What’s on your mind?"
                    className="compose-large"
                    maxLength={5000}
                    required
                  />
                </div>
                <div className="field">
                  <span>Publish to</span>
                  {conns.map((c) => (
                    <label key={c.id}>
                      <input
                        type="checkbox"
                        checked={compose.destinations.includes(c.id)}
                        disabled={
                          !!compose.replyId &&
                          !compose.relayParentDraftId &&
                          c.id !== compose.replyConnectionId
                        }
                        onChange={(e) =>
                          setCompose({
                            ...compose,
                            destinations: e.target.checked
                              ? [...compose.destinations, c.id]
                              : compose.destinations.filter(
                                  (id) => id !== c.id,
                                ),
                          })
                        }
                      />{" "}
                      {names[c.network]} · @{c.handle}
                    </label>
                  ))}
                  {!conns.length && (
                    <button
                      type="button"
                      className="secondary"
                      onClick={() => go("Settings")}
                    >
                      Connect an account first
                    </button>
                  )}
                </div>
                {relayCandidate && compose.replyId && (
                  <label className="field">
                    <span>
                      <input
                        type="checkbox"
                        checked={!!compose.relayParentDraftId}
                        onChange={(e) =>
                          setCompose({
                            ...compose,
                            relayParentDraftId: e.target.checked
                              ? relayCandidate
                              : undefined,
                            destinations: e.target.checked
                              ? compose.destinations
                              : [compose.replyConnectionId!],
                          })
                        }
                      />{" "}
                      Relay this reply to selected linked accounts with a
                      confirmed parent post
                    </span>
                  </label>
                )}
                {compose.replyId && (
                  <div className="notice">
                    Replying on the original network.{" "}
                    <button
                      type="button"
                      onClick={() =>
                        setCompose({
                          ...compose,
                          replyId: undefined,
                          replyCid: undefined,
                          replyConnectionId: undefined,
                          rootId: undefined,
                          rootCid: undefined,
                        })
                      }
                    >
                      Cancel reply
                    </button>
                  </div>
                )}
                <label className="field">
                  Schedule ({compose.timezone})
                  <input
                    type="datetime-local"
                    value={schedule}
                    onChange={(e) => setSchedule(e.target.value)}
                  />
                </label>
                <label className="field">
                  Attach still images (up to 4)
                  <input
                    type="file"
                    accept="image/png,image/jpeg,image/webp,image/gif"
                    multiple
                    disabled={busy}
                    onChange={(e) => {
                      const files = Array.from(e.target.files || []);
                      void act(async () => {
                        if (files.length + (compose.mediaIds?.length || 0) > 4)
                          throw new Error("Attach at most four images");
                        const ids = [...(compose.mediaIds || [])];
                        for (const file of files) {
                          const media = await api.uploadMedia(file);
                          ids.push(media.id);
                        }
                        setCompose({ ...compose, mediaIds: ids });
                      });
                    }}
                  />
                </label>
                <p className="meta">
                  Images are checked, resized to 1400 pixels and stored as PNG
                  with metadata removed. Animated uploads are not supported.
                </p>
                {compose.mediaIds?.map((id) => (
                  <div className="field" key={id}>
                    <div className="row">
                      <img
                        src={"/api/v1/media/" + id}
                        alt="Your attachment"
                        className="media-thumbnail"
                      />
                      <button
                        type="button"
                        onClick={() =>
                          setCompose({
                            ...compose,
                            mediaIds: compose.mediaIds?.filter((m) => m !== id),
                          })
                        }
                      >
                        Remove
                      </button>
                    </div>
                    <label>
                      Image description
                      <input
                        placeholder="Describe the image for accessibility"
                        onBlur={(e) => {
                          if (e.target.value)
                            void act(() =>
                              api.mediaAlt({ id }, { alt: e.target.value }),
                            );
                        }}
                      />
                    </label>
                  </div>
                ))}
                <div className="composer-footer">
                  <span className="meta">
                    {[...compose.text].length} characters
                  </span>
                  <div>
                    <button
                      type="button"
                      disabled={busy}
                      className="secondary"
                      onClick={() => void act(() => save())}
                    >
                      Save draft
                    </button>{" "}
                    <button
                      className="primary"
                      disabled={busy || !compose.destinations.length}
                    >
                      {busy ? "Working…" : schedule ? "Schedule" : "Publish"}{" "}
                      <Icon name="Arrow" />
                    </button>
                  </div>
                </div>
              </form>
            </>
          )}
          {page === "Notifications" && (
            <>
              <h1 className="title">A little connection.</h1>
              <p className="subtitle">The conversations you’re part of.</p>
              <button
                className="secondary"
                onClick={() => void notifications.refetch()}
              >
                Refresh
              </button>
              <Notices items={notifications.data?.warnings} />
              <div className="card">
                {notifications.data?.items.map((n) => (
                  <div className="notification" key={n.connectionId + n.id}>
                    <Icon name="Notifications" />
                    <div>
                      {n.text}
                      <div className="meta">
                        {names[n.network]} ·{" "}
                        {new Date(n.createdAt).toLocaleString()}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
              {notifications.isPending ? (
                <p role="status">Loading notifications…</p>
              ) : (
                !notifications.data?.items.length && (
                  <Empty title="You’re all caught up." />
                )
              )}
            </>
          )}
          {page === "Messages" && (
            <>
              <h1 className="title">Closer conversations.</h1>
              <p className="subtitle">
                A shared inbox. Every conversation stays on its own network.
              </p>
              <div className="notice">
                Threads messaging is unavailable through this integration.
                Mastodon private mentions are not end-to-end encrypted.
                Connected Bluesky accounts require a grant with native chat
                access. Hosted AT Protocol accounts use private Zing chat, with
                recipient acceptance. Zing chat is stored encrypted on this
                server and is not end-to-end encrypted or relayed to Bluesky.
              </div>
              <button
                className="secondary"
                onClick={() => {
                  void conversations.refetch();
                  void messages.refetch();
                }}
              >
                Refresh inbox
              </button>
              <Notices items={conversations.data?.warnings} />
              <div className="card chat">
                <div className="chat-list">
                  {conversations.data?.items.map((c) => (
                    <button
                      key={c.connectionId + c.id}
                      onClick={() => setConversation(c)}
                    >
                      <span>
                        <strong>{c.title}</strong>
                        <br />
                        <small>
                          {c.service === "zing"
                            ? "Zing chat · hosted AT Protocol"
                            : names[c.network]}
                        </small>
                        <br />
                        <span className="meta">{c.lastText.slice(0, 50)}</span>
                      </span>
                    </button>
                  ))}
                </div>
                <div className="conversation">
                  {conversation ? (
                    <>
                      <div className="row">
                        <strong>{conversation.title}</strong>
                        <span className={"network " + conversation.network}>
                          {conversation.service === "zing"
                            ? "Zing chat"
                            : names[conversation.network]}
                        </span>
                      </div>
                      {messages.data?.service === "zing" && (
                        <p className="meta">
                          Private Zing conversation. Messages stay on this
                          instance.
                        </p>
                      )}
                      {messages.data?.cursor && olderCursor !== "" && (
                        <button
                          className="secondary"
                          disabled={busy}
                          onClick={() =>
                            void act(async () => {
                              const before =
                                document.documentElement.scrollHeight;
                              const data = await api.messages({
                                query: {
                                  connectionId: conversation.connectionId,
                                  conversationId: conversation.id,
                                  cursor: olderCursor ?? messages.data!.cursor,
                                },
                              });
                              setOlderMessages((current) => [
                                ...data.items,
                                ...current,
                              ]);
                              setOlderCursor(data.cursor);
                              requestAnimationFrame(() =>
                                window.scrollBy(
                                  0,
                                  document.documentElement.scrollHeight -
                                    before,
                                ),
                              );
                            })
                          }
                        >
                          Load older messages
                        </button>
                      )}
                      {messages.isPending ? (
                        <p role="status">Loading conversation…</p>
                      ) : (
                        [...olderMessages, ...(messages.data?.items || [])]
                          .filter(
                            (m, i, all) =>
                              all.findIndex((item) => item.id === m.id) === i,
                          )
                          .map((m) => (
                            <div
                              key={m.id}
                              className={
                                "bubble " +
                                (conns.find(
                                  (c) => c.id === conversation.connectionId,
                                )?.subject === (m.senderDid || m.sender)
                                  ? "mine"
                                  : "")
                              }
                            >
                              <small className="meta">{m.sender}</small>
                              <div className="preserve">{m.text}</div>
                              {messages.data?.service === "zing" &&
                                m.senderDid !==
                                  conns.find(
                                    (c) => c.id === conversation.connectionId,
                                  )?.subject && (
                                  <button
                                    className="small"
                                    onClick={() => {
                                      setReportReason("");
                                      setAttachEvidence(false);
                                      setReporting({
                                        connectionId: conversation.connectionId,
                                        target: m.senderDid,
                                        kind: "message",
                                        messageId: m.id,
                                      });
                                    }}
                                  >
                                    Report this message
                                  </button>
                                )}
                            </div>
                          ))
                      )}
                      {messages.data?.canAccept && (
                        <button
                          className="primary"
                          disabled={busy}
                          onClick={() =>
                            void act(async () => {
                              await api.acceptZingConversation(
                                { id: conversation.id },
                                { connectionId: conversation.connectionId },
                              );
                              refresh("messages");
                              refresh("conversations");
                            }, "Conversation accepted")
                          }
                        >
                          Accept conversation
                        </button>
                      )}
                      {messages.data?.service === "zing" &&
                        !messages.data.canReply &&
                        !messages.data.canAccept && (
                          <p className="notice">
                            Replies are unavailable. Pending invitations require
                            acceptance; deleted accounts cannot receive replies.
                          </p>
                        )}
                      <MessageForm
                        key={conversation.connectionId + conversation.id}
                        disabled={
                          busy ||
                          messages.isPending ||
                          (messages.data?.service === "zing" &&
                            !messages.data.canReply)
                        }
                        send={(text) =>
                          act(async () => {
                            const key =
                              conversation.connectionId + ":" + conversation.id;
                            let attempt = pendingSends.current.get(key);
                            if (!attempt || attempt.text !== text) {
                              attempt = { text, id: crypto.randomUUID() };
                              pendingSends.current.set(key, attempt);
                            }
                            await api.sendMessage({
                              clientId: attempt.id,
                              connectionId: conversation.connectionId,
                              conversationId: conversation.id,
                              text,
                            });
                            pendingSends.current.delete(key);
                            refresh("messages");
                            refresh("conversations");
                          })
                        }
                      />
                    </>
                  ) : (
                    <Empty
                      title={
                        conversations.data?.items.length
                          ? "Pick a conversation."
                          : "Your inbox is quiet."
                      }
                    >
                      <p>
                        Existing conversations from connected networks appear
                        here.
                      </p>
                    </Empty>
                  )}
                </div>
              </div>
            </>
          )}
          {page === "Publishing" && (
            <>
              <h1 className="title">Ideas, in motion.</h1>
              <p className="subtitle">
                Drafts, scheduled posts, and the status of every destination.
              </p>
              <div className="tabs">
                {["Drafts", "Scheduled", "Delivery history"].map((t) => (
                  <button
                    key={t}
                    className={publicationTab === t ? "active" : ""}
                    onClick={() => setPublicationTab(t)}
                  >
                    {t}
                  </button>
                ))}
                <button
                  className="filter"
                  aria-label="Refresh publishing"
                  onClick={() => {
                    void drafts.refetch();
                    void deliveries.refetch();
                  }}
                >
                  <Icon name="Repost" />
                </button>
              </div>
              {publicationTab === "Delivery history"
                ? deliveries.data?.items.map((d) => (
                    <div className="card form-card" key={d.id}>
                      <div className="row">
                        <span className="name">
                          {names[
                            conns.find((c) => c.id === d.connectionId)
                              ?.network || ""
                          ] || "Disconnected account"}
                        </span>
                        <span className="pill">{d.state}</span>
                      </div>
                      <p className="meta">
                        {d.error || `${d.attempts} delivery attempt(s)`}
                      </p>
                      {safeURL(d.remoteUrl) && (
                        <a
                          href={safeURL(d.remoteUrl)}
                          target="_blank"
                          rel="noreferrer"
                        >
                          View published post ↗
                        </a>
                      )}
                      {d.state === "failed" && (
                        <button
                          className="secondary"
                          disabled={busy}
                          onClick={() =>
                            void act(async () => {
                              await api.retry({ id: d.id });
                              refresh("deliveries");
                            }, "Retry queued")
                          }
                        >
                          Retry this destination
                        </button>
                      )}
                      {d.state === "uncertain" && (
                        <p className="notice warning">
                          Check the destination before attempting another
                          publication. Automatic retry is disabled to avoid
                          duplicates.
                        </p>
                      )}
                    </div>
                  ))
                : drafts.data?.items
                    .filter((d) =>
                      publicationTab === "Scheduled"
                        ? d.status === "queued"
                        : d.status === "draft",
                    )
                    .map((d) => (
                      <div className="card form-card" key={d.id}>
                        <div className="row">
                          <span className="pill">{d.status}</span>
                          <span className="meta">
                            {d.scheduledAt
                              ? new Date(d.scheduledAt).toLocaleString()
                              : ""}
                          </span>
                        </div>
                        <p className="post-body">{d.text}</p>
                        <div className="row">
                          {d.status === "draft" && (
                            <button
                              className="secondary"
                              onClick={() => editDraft(d)}
                            >
                              Continue writing
                            </button>
                          )}
                          <button
                            className="secondary"
                            disabled={busy}
                            onClick={() =>
                              void act(async () => {
                                await api.cancel({ id: d.id });
                                refresh("drafts");
                                refresh("deliveries");
                              }, "Draft cancelled. A delivery already in progress may finish.")
                            }
                          >
                            Cancel
                          </button>
                        </div>
                      </div>
                    ))}
              {!drafts.isPending &&
                !deliveries.isPending &&
                !(publicationTab === "Delivery history"
                  ? deliveries.data?.items.length
                  : drafts.data?.items.filter((d) =>
                      publicationTab === "Scheduled"
                        ? d.status === "queued"
                        : d.status === "draft",
                    ).length) && (
                  <Empty title="Room for your next idea.">
                    <button className="primary" onClick={() => go("Compose")}>
                      Create a post
                    </button>
                  </Empty>
                )}
            </>
          )}
          {page === "Settings" && (
            <>
              <h1 className="title">Make yourself at home.</h1>
              <details className="card form-card">
                <summary>Suspension appeals</summary>
                <p>
                  If your account is suspended, you can submit one open appeal
                  to this instance’s moderators. Account deletion remains
                  available below.
                </p>
                <form
                  onSubmit={(e) => {
                    e.preventDefault();
                    void act(async () => {
                      await api.appeal({ reason: appealReason });
                      setAppealReason("");
                    }, "Appeal submitted");
                  }}
                >
                  <label>
                    Appeal reason
                    <textarea
                      required
                      maxLength={2000}
                      value={appealReason}
                      onChange={(e) => setAppealReason(e.target.value)}
                    />
                  </label>
                  <button className="secondary" disabled={busy}>
                    Submit appeal
                  </button>
                </form>
              </details>
              <p className="subtitle">
                Your accounts, your preferences, your corner of the internet.
              </p>
              <div className="card form-card">
                <h2 className="section-title">Your hosted identities</h2>
                <p className="meta">
                  One Zing account includes identities on this instance’s
                  configured social servers. Post and follow people without
                  signing up elsewhere.
                </p>
                {hosting.error && (
                  <p role="alert">Could not load hosted identity settings.</p>
                )}
                {hosting.data?.enrollment && (
                  <div className="notice">
                    <strong>Automatic account setup</strong>
                    <p>{hosting.data.enrollment.message}</p>
                    <div className="row">
                      <button
                        className="secondary"
                        onClick={() => {
                          void hosting.refetch();
                          void connections.refetch();
                        }}
                      >
                        Refresh identities
                      </button>
                      {hosting.data.enrollment.state === "partial" && (
                        <button
                          className="secondary"
                          disabled={busy}
                          onClick={() =>
                            void act(async () => {
                              await api.retryEnrollment();
                              refresh("hosting");
                            }, "Account setup retries queued")
                          }
                        >
                          Retry account setup
                        </button>
                      )}
                    </div>
                  </div>
                )}
                {hosting.data?.identities.map((identity) => (
                  <div className="connection" key={identity.id}>
                    <div>
                      <div className="name">
                        {identity.network === "mastodon"
                          ? "ActivityPub"
                          : "Bluesky PDS"}
                      </div>
                      <div className="meta">
                        {identity.handle || identity.username} ·{" "}
                        {identity.state}
                      </div>
                      {identity.error && <p role="alert">{identity.error}</p>}
                    </div>
                    {identity.state === "active" && (
                      <button
                        className="secondary push-right"
                        onClick={() => {
                          const c = conns.find(
                            (c) => c.hosted && c.network === identity.network,
                          );
                          setCompose({
                            ...blank(),
                            destinations: c ? [c.id] : [],
                          });
                          go("Compose");
                        }}
                      >
                        Write a post
                      </button>
                    )}
                  </div>
                ))}
                {!hosting.data?.enrollment &&
                hosting.data?.networks.some((n) => n.available) ? (
                  <form
                    className="stack"
                    onSubmit={(event) => {
                      event.preventDefault();
                      void act(async () => {
                        await api.createHostedIdentity({
                          network: hostedNetwork,
                          username: hostedUsername,
                        });
                        setHostedUsername("");
                        refresh("hosting");
                        refresh("connections");
                        refresh("feed");
                      }, "Your hosted identity is ready.").then(() =>
                        refresh("hosting"),
                      );
                    }}
                  >
                    <label className="field">
                      Hosted network
                      <select
                        aria-label="Hosted network"
                        value={hostedNetwork}
                        onChange={(event) =>
                          setHostedNetwork(event.target.value)
                        }
                      >
                        {hosting.data.networks.map((n) => (
                          <option
                            key={n.network}
                            value={n.network}
                            disabled={!n.available}
                          >
                            {n.network === "mastodon"
                              ? "ActivityPub"
                              : "Bluesky PDS"}
                            {n.available ? " · " + n.domain : " · unavailable"}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label className="field">
                      Username
                      <input
                        required
                        minLength={hostedNetwork === "bluesky" ? 3 : 1}
                        maxLength={hostedNetwork === "bluesky" ? 18 : 30}
                        pattern={
                          hostedNetwork === "bluesky"
                            ? "[a-z0-9]{3,18}"
                            : "[a-z0-9][a-z0-9_]{0,29}"
                        }
                        autoCapitalize="none"
                        autoComplete="off"
                        value={hostedUsername}
                        onChange={(event) =>
                          setHostedUsername(event.target.value)
                        }
                      />
                    </label>
                    <p className="meta">
                      One hosted identity per network. Names and identity
                      domains are permanent. PDS posts are public repository
                      records; local PDS hosting does not include Bluesky chat.
                    </p>
                    <button
                      className="primary"
                      disabled={
                        busy ||
                        !hostedUsername ||
                        hosting.data.identities.some(
                          (i) =>
                            i.network === hostedNetwork && i.state === "active",
                        )
                      }
                    >
                      Create hosted identity
                    </button>
                  </form>
                ) : (
                  <p className="meta">
                    {hosting.data?.enrollment
                      ? "Your identities are managed automatically with your Zing account."
                      : "The operator has not configured social-server hosting yet."}
                  </p>
                )}
              </div>
              <div className="card form-card">
                <h2 className="section-title">Connected accounts</h2>
                {conns.map((c) => (
                  <div className="connection" key={c.id}>
                    <span className={"network-logo " + c.network}>
                      {names[c.network]?.slice(0, 1)}
                    </span>
                    <div>
                      <div className="name">
                        {names[c.network]}
                        {c.hosted ? " · Hosted here" : ""}
                      </div>
                      <div className="meta">
                        @{c.handle}
                        {c.reconnect ? " · Reconnect needed" : ""}
                      </div>
                    </div>
                    <button
                      className="secondary push-right"
                      disabled={busy || c.hosted}
                      onClick={() => {
                        if (
                          window.confirm(
                            `Disconnect @${c.handle}? Pending deliveries to it will be cancelled.`,
                          )
                        )
                          void act(async () => {
                            await api.disconnect({ id: c.id });
                            refresh("connections");
                          }, "Account disconnected");
                      }}
                    >
                      {c.hosted ? "Hosted here" : "Disconnect"}
                    </button>
                  </div>
                ))}
                <div className="stack">
                  {Object.keys(names).map((n) => (
                    <button
                      key={n}
                      className="secondary full"
                      onClick={() => setConnecting(n)}
                    >
                      + Connect {names[n]}
                    </button>
                  ))}
                </div>
              </div>
              <div className="card form-card">
                <h2 className="section-title">Appearance</h2>
                <label className="field">
                  Theme
                  <select
                    aria-label="Theme"
                    value={theme}
                    onChange={(e) => setTheme(e.target.value)}
                  >
                    <option value="system">Follow device</option>
                    <option value="light">Light</option>
                    <option value="dark">Dark</option>
                  </select>
                </label>
              </div>
              <div className="card form-card">
                <h2 className="section-title">Account & security</h2>
                <p className="subtitle">{user.email}</p>
                <div className="stack">
                  <a href="/forgot" className="secondary">
                    Reset password
                  </a>
                  <button
                    className="secondary"
                    disabled={busy}
                    onClick={() =>
                      void act(async () => {
                        await api.authLogout();
                        queryClient.clear();
                        localStorage.removeItem(
                          "zing-unsaved:" + user?.subject,
                        );
                        setCompose(blank());
                        refresh("session");
                      })
                    }
                  >
                    Sign out
                  </button>
                  <button
                    className="secondary danger"
                    disabled={busy}
                    onClick={() => {
                      const password = window.prompt(
                        "Enter your password to delete your Zing account and request deletion of identities hosted here. Posts on independently connected accounts remain there.",
                      );
                      if (password)
                        void act(async () => {
                          await api.authDelete({ password });
                          queryClient.clear();
                          localStorage.removeItem(
                            "zing-unsaved:" + user?.subject,
                          );
                          refresh("session");
                        });
                    }}
                  >
                    Delete Zing account
                  </button>
                </div>
              </div>
              <p className="meta">
                <a href="/privacy">Privacy</a> · <a href="/terms">Terms</a> ·{" "}
                <a href="/admin">Operator settings</a>
              </p>
            </>
          )}
        </main>
        <aside className="rightbar">
          <form
            className="searchbox"
            onSubmit={(e) => {
              e.preventDefault();
              setSearch(searchInput);
              go("Search");
            }}
          >
            <Icon name="Search" />
            <input
              aria-label="Search connected networks"
              placeholder="Search the connected world"
              value={searchInput}
              onChange={(e) => setSearchInput(e.target.value)}
            />
          </form>
          <div className="card right-card">
            <div className="section-heading">
              <h2>Your worlds</h2>
              <button className="small" onClick={() => go("Settings")}>
                Manage
              </button>
            </div>
            {conns.length ? (
              conns.map((c) => <ConnectionRow key={c.id} connection={c} />)
            ) : (
              <p className="subtitle small">
                Create your identity here, then follow people and start a
                conversation.
              </p>
            )}
            <button className="secondary full" onClick={() => go("Settings")}>
              <Icon name="Plus" />{" "}
              {hosting.data?.networks.some((n) => n.available)
                ? "Manage identities"
                : "Connect an account"}
            </button>
          </div>
          <div className="card right-card">
            <h2 className="section-title">A more intentional internet.</h2>
            <p className="subtitle small">
              People, ideas, and conversations from the worlds you choose.
              You’re in control of where you post.
            </p>
            <div className="notice">
              Replies and private messages stay on their original network.
            </div>
          </div>
          <div className="footer">
            <a href="/privacy">Privacy</a> · <a href="/terms">Terms</a>
            <br />© 2026 Zing · A little more connected.
          </div>
        </aside>
      </div>
      {connecting && (
        <ConnectDialog
          network={connecting}
          close={() => setConnecting("")}
          done={() => {
            refresh("connections");
            setConnecting("");
          }}
        />
      )}
      {reporting && (
        <Dialog
          title="Report to this Zing instance"
          onClose={() => setReporting(null)}
        >
          <form
            className="stack"
            onSubmit={(e) => {
              e.preventDefault();
              void act(async () => {
                await api.report({ ...reporting, reason: reportReason });
                setReporting(null);
              }, "Report sent to the instance moderators");
            }}
          >
            <p>The report stays with this installation’s moderators.</p>
            {reporting.kind === "message" && (
              <label>
                <input
                  type="checkbox"
                  required
                  checked={attachEvidence}
                  onChange={(e) => setAttachEvidence(e.target.checked)}
                />
                Attach this selected private message and its sender to the
                report. No other messages are included.
              </label>
            )}
            <label>
              Reason
              <textarea
                required
                maxLength={2000}
                value={reportReason}
                onChange={(e) => setReportReason(e.target.value)}
              />
            </label>
            <ErrorNotice error={failure} />
            <button className="primary" disabled={busy}>
              Send report
            </button>
          </form>
        </Dialog>
      )}
      {profile && (
        <Dialog
          title={profile.name || profile.handle}
          onClose={() => setProfile(null)}
        >
          <Avatar name={profile.name || profile.handle} src={profile.avatar} />
          <p className="meta">@{profile.handle}</p>
          <p className="post-body">{profile.bio}</p>
          <button
            className="small"
            onClick={() => {
              setReportReason("");
              setReporting({
                connectionId: profile.connectionId,
                target: profile.id,
                kind: "account",
              });
              setProfile(null);
            }}
          >
            Report account
          </button>
          <p>
            {profile.followers} followers · {profile.following} following
          </p>
          <button
            className="primary"
            disabled={busy}
            onClick={() =>
              void act(
                () =>
                  api.action({
                    connectionId: profile.connectionId,
                    action: "follow",
                    target: profile.id,
                  }),
                "Follow request sent",
              )
            }
          >
            Follow
          </button>{" "}
          <button
            className="secondary"
            disabled={
              busy ||
              !conns
                .find((c) => c.id === profile.connectionId)
                ?.capabilities.some(
                  (capability) =>
                    capability === "conversations" ||
                    capability === "zing-chat",
                )
            }
            onClick={() =>
              void act(async () => {
                const c = await api.openConversation({
                  connectionId: profile.connectionId,
                  recipient: profile.id,
                });
                setConversation(c);
                setProfile(null);
                go("Messages");
              })
            }
          >
            Message
          </button>
        </Dialog>
      )}
      {postMenu && (
        <Dialog title="Post options" onClose={() => setPostMenu(null)}>
          <button
            className="secondary"
            onClick={() => {
              setReportReason("");
              setReporting({
                connectionId: postMenu.connectionId,
                target: postMenu.id,
                kind: "post",
              });
              setPostMenu(null);
            }}
          >
            Report post
          </button>
          <div className="stack">
            {["mute", "block"].map((a) => (
              <button
                className="secondary"
                key={a}
                disabled={
                  busy ||
                  !conns
                    .find((c) => c.id === postMenu.connectionId)
                    ?.capabilities.includes(a)
                }
                onClick={() =>
                  void act(async () => {
                    await action(postMenu, a, postMenu.author.id);
                    setPostMenu(null);
                  })
                }
              >
                {a === "mute" ? "Mute account" : "Block account"}
              </button>
            ))}
          </div>
        </Dialog>
      )}
      {notice && (
        <div className="toast" role="status">
          {notice}
        </div>
      )}
    </>
  );
  function editDraft(d: DraftView) {
    setCompose({
      ...d.composition,
      text: d.text,
      destinations: d.destinations.filter((id) =>
        conns.some((c) => c.id === id),
      ),
      mediaIds: d.mediaIds,
      timezone: d.timezone,
    });
    setRelayCandidate(d.composition?.relayParentDraftId || "");
    setDraftId(d.id);
    setSchedule(d.scheduledAt ? toLocalInput(d.scheduledAt) : "");
    go("Compose");
  }
}
function toLocalInput(value: string) {
  const d = new Date(value);
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000)
    .toISOString()
    .slice(0, 16);
}
function Brand() {
  return (
    <a className="brand" href="/">
      <img src="/assets/zing-icon.svg" alt="Zing logo" />
      Zing<span className="brand-dot">.</span>
    </a>
  );
}
function ConnectionRow({ connection: c }: { connection: ConnectionView }) {
  return (
    <div className="connection">
      <span className={"network-logo " + c.network}>
        {names[c.network]?.slice(0, 1)}
      </span>
      <div>
        <div className="name small">
          {names[c.network]}
          {c.hosted ? " · Hosted here" : ""}
        </div>
        <div className="meta">@{c.handle}</div>
      </div>
      <span className={c.reconnect ? "pill" : "dot"}>
        {c.reconnect ? "!" : ""}
      </span>
    </div>
  );
}
function MessageForm({
  send,
  disabled,
}: {
  send: (text: string) => Promise<boolean | undefined>;
  disabled: boolean;
}) {
  const [text, setText] = useState("");
  return (
    <form
      className="message-form"
      onSubmit={(e) => {
        e.preventDefault();
        void send(text).then((ok) => {
          if (ok) setText("");
        });
      }}
    >
      <input
        aria-label="Message"
        value={text}
        onChange={(e) => setText(e.target.value)}
        required
        maxLength={1000}
        placeholder="Write a message…"
      />
      <button
        disabled={disabled || !text.trim()}
        className="primary"
        aria-label="Send message"
      >
        <Icon name="Arrow" />
      </button>
    </form>
  );
}
function ConnectDialog({
  network,
  close,
  done,
}: {
  network: string;
  close: () => void;
  done: () => void;
}) {
  const [error, setError] = useState<unknown>(),
    [busy, setBusy] = useState(false);
  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    setBusy(true);
    try {
      const result = await api.connect({
        network,
        server: String(form.get("server") || ""),
        handle: String(form.get("handle") || ""),
        appPassword: String(form.get("password") || ""),
      });
      if (result.url.startsWith("/")) done();
      else window.location.assign(result.url);
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog title={"Connect " + names[network]} onClose={close}>
      <ErrorNotice error={error} />
      <form onSubmit={(e) => void submit(e)}>
        {network === "mastodon" && (
          <label className="field">
            Your Mastodon server
            <input name="server" placeholder="mastodon.social" required />
          </label>
        )}
        {network === "bluesky" && (
          <>
            <label className="field">
              Handle
              <input
                name="handle"
                placeholder="you.bsky.social"
                required
                autoComplete="off"
              />
            </label>
            <label className="field">
              App password (optional; leave blank for OAuth)
              <input type="password" name="password" autoComplete="off" />
            </label>
            <label className="field">
              PDS entry server
              <input name="server" placeholder="bsky.social" />
            </label>
            <p className="notice">
              Leave the password blank to authorize securely with Bluesky. For
              local development, use an app password with chat access. Never
              enter your main account password.
            </p>
          </>
        )}
        {network === "threads" && (
          <div className="notice">
            The operator must configure a Threads developer app and approved
            permissions. Threads home feeds and messaging are not exposed here.
          </div>
        )}
        <button className="primary full" disabled={busy}>
          {busy
            ? "Connecting…"
            : network === "bluesky"
              ? "Connect securely"
              : "Continue to " + names[network]}
        </button>
      </form>
    </Dialog>
  );
}
function AuthPage({ path, onDone }: { path: string; onDone: () => void }) {
  const [mode, setMode] = useState(path === "/" ? "login" : path.slice(1)),
    [error, setError] = useState<unknown>(),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(false);
  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    const email = String(f.get("email") || ""),
      password = String(f.get("password") || ""),
      token = String(f.get("token") || "");
    setBusy(true);
    setError(undefined);
    try {
      if (mode === "login") {
        await api.authLogin({ email, password });
        onDone();
      } else if (mode === "register") {
        await api.authRegister({
          email,
          password,
          name: String(f.get("name") || ""),
        });
        onDone();
      } else if (mode === "forgot") {
        await api.authForgot({ email });
        setNotice("If that account exists, a reset link has been sent.");
      } else if (mode === "reset") {
        await api.authReset({ token, password });
        setMode("login");
        setNotice("Password updated. Sign in with your new password.");
      } else {
        await api.authVerify({ token });
        setNotice("Email verified.");
        onDone();
      }
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }
  const title =
    mode === "register"
      ? "Find your people."
      : mode === "forgot"
        ? "A fresh start."
        : mode === "reset"
          ? "Set a new password."
          : mode === "verify"
            ? "A little verification."
            : "Welcome back.";
  const token =
    typeof window === "undefined"
      ? ""
      : new URLSearchParams(window.location.search).get("token") || "";
  return (
    <div className="auth">
      <Brand />
      <div className="card form-card">
        <div className="eyebrow">YOUR WORLD, CONNECTED</div>
        <h1 className="title">{title}</h1>
        <p className="subtitle">A little more connected, starting here.</p>
        <ErrorNotice error={error} />
        {notice && (
          <div className="notice" role="status">
            {notice}
          </div>
        )}
        <form onSubmit={(e) => void submit(e)}>
          {mode === "register" && (
            <label className="field">
              Name
              <input name="name" autoComplete="name" required maxLength={200} />
            </label>
          )}
          {["login", "register", "forgot"].includes(mode) && (
            <label className="field">
              Email
              <input name="email" type="email" autoComplete="email" required />
            </label>
          )}
          {["login", "register", "reset"].includes(mode) && (
            <label className="field">
              Password
              <input
                name="password"
                type="password"
                autoComplete={
                  mode === "login" ? "current-password" : "new-password"
                }
                minLength={mode === "login" ? 1 : 10}
                maxLength={200}
                required
              />
            </label>
          )}
          {["verify", "reset"].includes(mode) && (
            <label className="field">
              Email token
              <input
                name="token"
                defaultValue={token}
                required
                autoComplete="off"
              />
            </label>
          )}
          <button className="primary full" disabled={busy}>
            {busy
              ? "Working…"
              : mode === "register"
                ? "Create account"
                : mode === "forgot"
                  ? "Send reset link"
                  : mode === "verify"
                    ? "Verify email"
                    : "Continue"}
            <Icon name="Arrow" />
          </button>
        </form>
        <p className="center">
          <button
            onClick={() => {
              setMode(mode === "login" ? "register" : "login");
              setError(undefined);
            }}
          >
            {mode === "login"
              ? "New here? Create an account"
              : "Already here? Sign in"}
          </button>
        </p>
        {mode === "login" && (
          <button className="full meta" onClick={() => setMode("forgot")}>
            Forgot your password?
          </button>
        )}
      </div>
      <p className="meta center">
        <a href="/setup">First-run operator setup</a>
        <br />
        {mode === "register" && (
          <span>
            Your verified Zing signup automatically creates identities on this
            instance’s configured ActivityPub and Bluesky servers.{" "}
          </span>
        )}
        By continuing, you accept the <a href="/terms">terms</a> and{" "}
        <a href="/privacy">privacy policy</a>.
      </p>
    </div>
  );
}
