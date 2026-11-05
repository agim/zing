export function Privacy() {
  return (
    <Legal title="Privacy">
      <p>
        Zing stores your account details, encrypted network credentials, drafts,
        uploaded media, schedules, and delivery records to provide the service.
        Your feed and conversations are retrieved from the networks you connect.
      </p>
      <p>
        Credentials remain on the server. Private messages stay on their
        original network and are never automatically cross-posted. Mastodon
        private mentions are not end-to-end encrypted.
      </p>
      <p>
        Disconnect an account in Settings to remove its Zing credentials and
        cancel pending deliveries. Delete your Zing account to remove
        application records and queue media removal. Posts already published on
        other networks remain there and must be deleted on those networks.
      </p>
      <p>
        The service operator must publish their contact details and
        retention/backup policy before opening public registration.
      </p>
    </Legal>
  );
}
export function Terms() {
  return (
    <Legal title="Terms">
      <p>
        Connect only accounts you control. Choose each publication destination
        explicitly and respect the policies of the networks you use. Zing cannot
        guarantee another network’s availability or support operations that its
        API does not provide.
      </p>
      <p>
        Scheduled and relayed posts are delivered independently to each
        destination. A delivery that cannot be confirmed remains uncertain until
        reconciled; publishing it again may create a duplicate.
      </p>
      <p>
        The operator must finalize these terms, service contact, and abuse
        reporting procedure before public launch.
      </p>
    </Legal>
  );
}
function Legal({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <main className="auth card form-card">
      <a className="brand" href="/">
        <img src="/assets/zing-icon.svg" alt="Zing logo" />
        Zing.
      </a>
      <h1 className="title">{title}</h1>
      <div className="subtitle">{children}</div>
      <a href="/">Back to Zing</a>
    </main>
  );
}
