import { tr } from "./i18n";
import { useCallback, useEffect, useState } from "react";
import { useSessionScope } from "./useSessionScope";
type Status = {
  ldap: { configured: boolean; enabled: boolean; provider: string };
  sources: unknown[];
};
export function DirectorySettings() {
  const { api } = useSessionScope();
  const [state, setState] = useState<Status | null>(null),
    [busy, setBusy] = useState(false),
    [message, setMessage] = useState("");
  const load = useCallback(async () => {
    try {
      setState(await api<Status>("/directory/status"));
    } catch (e) {
      setMessage((e as Error).message);
    }
  }, [api]);
  useEffect(() => {
    void load();
  }, [load]);
  return (
    <article className="panel">
      <h2>{tr("m0587f2f46f")}</h2>
      <p>{tr("mc4327e130e")}</p>
      <span className={"badge " + (state?.ldap.enabled ? "green" : "gray")}>
        {state?.ldap.enabled ? tr("mebd7ada3e5") : tr("m399e562eb5")}
      </span>
      <p>
        {tr("mbb74f029b8")} {state?.ldap.provider || "—"}
      </p>
      <button
        disabled={busy || !state?.ldap.enabled}
        onClick={async () => {
          setBusy(true);
          setMessage("");
          try {
            await api("/directory/ldap/sync", "POST", {});
            await load();
            setMessage(tr("mc25f2e6330"));
          } catch (e) {
            setMessage((e as Error).message);
          } finally {
            setBusy(false);
          }
        }}
      >
        {busy ? tr("mea23d5d1f6") : tr("mce3fac5784")}
      </button>
      <p className="muted">{tr("mffbc0aa6f2")}</p>
      {message && (
        <p role="status" className="notice">
          {message}
        </p>
      )}
    </article>
  );
}
