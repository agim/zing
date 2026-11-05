import { useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@lidza/client";
export function Setup() {
  const cache = useQueryClient();
  const status = useQuery({
    queryKey: ["installation"],
    queryFn: () => api.status(),
    retry: false,
  });
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () => api.authSession(),
    retry: false,
  });
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    setBusy(true);
    setError("");
    try {
      if (!session.data?.user) {
        await api.authRegister({
          email: String(form.get("email")),
          name: String(form.get("name")),
          password: String(form.get("password")),
        });
        await cache.invalidateQueries({ queryKey: ["session"] });
      } else {
        await api.authOwnerClaim({ token: String(form.get("token")) });
        await cache.invalidateQueries({ queryKey: ["installation"] });
      }
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Could not finish this step",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="auth card form-card">
      <a className="brand" href="/">
        <img src="/assets/zing-icon.svg" alt="Zing logo" />
        Zing.
      </a>
      <h1 className="title">Your own little world.</h1>
      <p className="subtitle">
        First-run setup for this installation. Choose your own domain, access
        policy, mail provider, and storage. Settings and credentials are saved
        encrypted in PostgreSQL.
      </p>
      {error && (
        <div className="notice warning" role="alert">
          {error}
        </div>
      )}
      {status.error && (
        <div className="notice warning" role="alert">
          Could not read installation status. Try again.
        </div>
      )}
      {status.isPending || session.isPending ? (
        <p role="status">Checking installation…</p>
      ) : status.data?.configured ? (
        <>
          <div className="notice">
            This installation is configured. Only its operator can change
            instance settings.
          </div>
          <a className="primary full" href="/admin/installation">
            Open operator settings
          </a>
          <p>
            <a href="/">Back to Zing</a>
          </p>
        </>
      ) : !session.data?.user ? (
        <>
          <h2 className="section-title">1. Create your operator account</h2>
          <p className="meta">
            Creating an account does not grant operator access. The next step
            requires the installation’s one-time owner token.
          </p>
          <form onSubmit={(e) => void submit(e)}>
            <label className="field">
              Name
              <input name="name" autoComplete="name" required />
            </label>
            <label className="field">
              Email
              <input name="email" type="email" autoComplete="email" required />
            </label>
            <label className="field">
              Password
              <input
                name="password"
                type="password"
                autoComplete="new-password"
                minLength={10}
                required
              />
            </label>
            <button className="primary full" disabled={busy}>
              Create account
            </button>
          </form>
          <p>
            <a href="/login">Already registered? Sign in</a>
          </p>
        </>
      ) : status.data?.ownerClaimEnabled && !status.data.ownerClaimed ? (
        <>
          <h2 className="section-title">2. Claim this installation</h2>
          <p className="subtitle">
            In Līdza Deploy, open the app’s Operator setup token action. On
            another host, retrieve the owner-claim token from the protected
            server state. Never share it with another visitor.
          </p>
          <form onSubmit={(e) => void submit(e)}>
            <label className="field">
              One-time owner token
              <input type="password" name="token" autoComplete="off" required />
            </label>
            <button className="primary full" disabled={busy}>
              Claim operator access
            </button>
          </form>
        </>
      ) : (
        <>
          <h2 className="section-title">3. Choose your services</h2>
          <p className="subtitle">
            Configure mail, storage, your origin, and private/public access.
            Secrets are masked after saving; the owner’s token never becomes a
            reusable credential.
          </p>
          <a className="primary full" href="/admin/installation">
            Continue to first-run settings
          </a>
        </>
      )}
    </main>
  );
}
