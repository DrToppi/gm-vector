import { tr } from "./i18n";
import { useSessionScope } from "./useSessionScope";
import { useEffect, useState } from "react";
import { QRCodeSVG } from "qrcode.react";
let initialInviteLogin =
  new URLSearchParams(location.hash.slice(1)).get("login") || "";
export function Auth({
  setup,
  onReady,
}: {
  setup: boolean;
  onReady: () => void;
}) {
  const scope = useSessionScope();
  const api = scope.api;
  const [enroll, setEnroll] = useState(
    setup || location.hash.startsWith("#setup="),
  );
  const [token, setToken] = useState(() => {
    const v = setup
      ? ""
      : new URLSearchParams(location.hash.slice(1)).get("setup") || "";
    history.replaceState(null, "", location.pathname);
    return v;
  });
  const [username, setUsername] = useState(() => {
    const login = initialInviteLogin;
    initialInviteLogin = "";
    return login;
  });
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [secret, setSecret] = useState("");
  const [uri, setURI] = useState("");
  const [recovery, setRecovery] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [initialResume] = useState(() => ({ enroll, token }));
  useEffect(() => {
    if (!initialResume.enroll || !initialResume.token) return;
    setBusy(true);
    api("/auth/bootstrap/resume", "POST", { token: initialResume.token })
      .then((r) => {
        setSecret(r.secret);
        setURI(r.otpauth_uri);
        setUsername(r.username);
      })
      .catch(() => {})
      .finally(() => setBusy(false));
  }, [api, initialResume]);
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      if (enroll && !secret) {
        const r = await api("/auth/bootstrap", "POST", {
          token,
          username,
          name,
          password,
        });
        setToken(r.enrollment_token || token);
        setSecret(r.secret);
        setURI(r.otpauth_uri);
        setPassword("");
      } else if (enroll) {
        const r = await api("/auth/bootstrap/confirm", "POST", { token, code });
        setToken("");
        setSecret("");
        setRecovery(r.recovery_codes);
      } else {
        await api("/auth/login", "POST", { username, password, code });
        setPassword("");
        onReady();
      }
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="auth">
      <section className="auth-intro">
        <div className="brand">
          gm<span>Vector</span>
        </div>
        <h1>
          {tr("m27709c63fb")}
          <br />
          {tr("mf3dc2e97c7")}
        </h1>
        <p>{tr("m68e1819856")}</p>
        <div className="auth-visual">
          <i />
          <i />
          <i />
          <i />
        </div>
      </section>
      <section className="auth-card">
        {recovery.length ? (
          <>
            <h2>{tr("md748ad4dfd")}</h2>
            <p>{tr("m627cc80399")}</p>
            <pre>{recovery.join("\n")}</pre>
            <button onClick={onReady}>{tr("mbb020ea83a")}</button>
          </>
        ) : (
          <>
            <p className="eyebrow">{tr("m0d7f700390")}</p>
            <h2>{enroll ? tr("md74489dc8f") : tr("ma4d4f34d1b")}</h2>
            <p>{enroll ? tr("m0f7956aeb2") : tr("me79283af7c")}</p>
            <form onSubmit={submit}>
              {enroll && !setup && !secret && (
                <label>
                  {tr("md8b63f6a12")}
                  <input
                    value={token}
                    onChange={(e) => setToken(e.target.value)}
                    required
                    autoComplete="off"
                    type="password"
                  />
                  <small>{tr("mc3038b4cc0")}</small>
                </label>
              )}
              {!secret && (
                <>
                  <label>
                    {tr("m5be12bfe49")}
                    <input
                      autoComplete="username"
                      value={username}
                      onChange={(e) => setUsername(e.target.value)}
                      required
                    />
                  </label>
                  {enroll && (
                    <label>
                      {tr("m1da7e9372e")}
                      <input
                        value={name}
                        onChange={(e) => setName(e.target.value)}
                        required
                      />
                    </label>
                  )}
                  <label>
                    {tr("mcb1a2074b3")}
                    <input
                      type="password"
                      autoComplete={
                        enroll ? "new-password" : "current-password"
                      }
                      maxLength={128}
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                      required
                    />
                  </label>
                </>
              )}
              {secret && (
                <div className="notice">
                  <b>{tr("m526fb635e5")}</b>
                  {uri && (
                    <div className="totp-qr">
                      <QRCodeSVG
                        value={uri}
                        size={220}
                        marginSize={4}
                        level="M"
                        title={tr("mfd21eecf11")}
                      />
                    </div>
                  )}
                  <p>{tr("m6a1eb7315f")}</p>
                  <details>
                    <summary>{tr("m5ef568a87c")}</summary>
                    <code className="secret">{secret}</code>
                    <button
                      type="button"
                      className="secondary"
                      onClick={() => navigator.clipboard.writeText(secret)}
                    >
                      {tr("m4400ee2244")}
                    </button>
                    <p>{tr("m9618e58c59")}</p>
                  </details>
                  <p>{tr("m861f01cf22")}</p>
                </div>
              )}
              {(!enroll || secret) && (
                <label>
                  {tr("m2f4dea7161")}
                  <input
                    value={code}
                    onChange={(e) => setCode(e.target.value.replace(/\s/g, ""))}
                    autoComplete="one-time-code"
                    inputMode={enroll ? "numeric" : "text"}
                    required
                  />
                </label>
              )}
              {error && (
                <p role="alert" className="error">
                  {error}
                </p>
              )}
              <button disabled={busy}>
                {busy
                  ? tr("m74d0346f0c")
                  : enroll && !secret
                    ? tr("m9ea7ec347e")
                    : enroll
                      ? tr("m27bca309f5")
                      : tr("m8fc9ab2eeb")}
              </button>
            </form>
            {!setup && !secret && (
              <button
                className="text-button"
                onClick={() => setEnroll(!enroll)}
              >
                {enroll ? tr("mdc4d306540") : tr("m194b06e948")}
              </button>
            )}
          </>
        )}
      </section>
    </main>
  );
}
