import { useCallback, useEffect, useRef, useState } from "react";
import { tr } from "./i18n";
import { useSessionScope } from "./useSessionScope";
type DirectorySource = { provider: string; kind: "ldap" | "api" };
export function DirectorySearch({ onRefresh }: { onRefresh: () => void }) {
  const scope = useSessionScope();
  const api = scope.api;
  const [q, setQ] = useState(""),
    [source, setSource] = useState<"ldap" | "api">("ldap"),
    [provider, setProvider] = useState("");
  const [sources, setSources] = useState<DirectorySource[]>([]),
    [rows, setRows] = useState<
      { id: string; name: string; email: string; provider: string }[]
    >([]),
    [msg, setMsg] = useState(""),
    [busy, setBusy] = useState(false);
  const version = useRef(0);
  const invalidateSearch = useCallback(() => {
    version.current++;
  }, []);
  useEffect(() => {
    let active = true;
    api<{ source_details: DirectorySource[] }>("/directory/status")
      .then((value) => {
        if (active) setSources(value.source_details || []);
      })
      .catch((e) => {
        if (active) setMsg(e.message);
      });
    return () => {
      active = false;
      invalidateSearch();
    };
  }, [api, invalidateSearch]);
  function changeSource(next: "ldap" | "api") {
    version.current++;
    setSource(next);
    setProvider("");
    setRows([]);
    setMsg("");
    setBusy(false);
  }
  async function search() {
    const ticket = ++version.current;
    setBusy(true);
    try {
      const params = new URLSearchParams({
        q,
        source,
        ...(provider ? { provider } : {}),
      });
      const result = await api("/directory/people?" + params);
      if (ticket === version.current) {
        setRows(result);
        setMsg(result.length ? "" : tr("directoryEmpty"));
      }
    } catch (e) {
      if (ticket === version.current) setMsg((e as Error).message);
    } finally {
      if (ticket === version.current) setBusy(false);
    }
  }
  const choices = sources.filter((item) => item.kind === source);
  return (
    <>
      <h3>{tr("m0074c5a16c")}</h3>
      <div className="toolbar">
        <label>
          {tr("directorySource")}
          <select
            value={source}
            onChange={(e) => changeSource(e.target.value as "ldap" | "api")}
          >
            <option value="ldap">LDAP</option>
            <option value="api">{tr("directoryAPI")}</option>
          </select>
        </label>
        {choices.length > 0 && (
          <label>
            {tr("directoryProvider")}
            <select
              value={provider}
              onChange={(e) => {
                version.current++;
                setProvider(e.target.value);
                setRows([]);
                setMsg("");
                setBusy(false);
              }}
            >
              <option value="">{tr("directoryAllProviders")}</option>
              {choices.map((item) => (
                <option key={item.provider} value={item.provider}>
                  {item.provider}
                </option>
              ))}
            </select>
          </label>
        )}
        <input
          aria-label={tr("m20ee7e744c")}
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder={tr("m20ee7e744c")}
        />
        <button
          className="secondary"
          disabled={busy}
          onClick={() => void search()}
        >
          {busy ? tr("m74d0346f0c") : tr("mbe6156b8e4")}
        </button>
      </div>
      {msg && <p role="status">{msg}</p>}
      {rows.map((r) => (
        <div className="assignment" key={r.provider + "/" + r.id}>
          <span>
            {r.name}
            <small> · {r.provider}</small>
          </span>
          <button
            className="secondary"
            onClick={async () => {
              try {
                await api("/employees", "POST", {
                  id: "",
                  name: r.name,
                  daily_minutes: 480,
                  user_id: "",
                  provider: r.provider,
                  external_id: r.id,
                });
                setRows((old) =>
                  old.filter((x) => x.id !== r.id || x.provider !== r.provider),
                );
                onRefresh();
              } catch (e) {
                setMsg((e as Error).message);
              }
            }}
          >
            {tr("m71038c53bb")}
          </button>
        </div>
      ))}
    </>
  );
}
