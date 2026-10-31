import React, { useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  api as rootAPI,
  setCSRF,
  advanceSession,
  isCurrentSession,
  sessionEpoch,
} from "./api";
import { tr, changeLocale, isLocale } from "./i18n";
import { useTranslation } from "react-i18next";
import { LanguagePicker } from "./LanguagePicker";
import { BugReport } from "./BugReport";
import { installDiagnostics, setDiagnosticIdentity } from "./diagnostics";
import { PlanningClockProvider } from "./PlanningClock";
import { Auth } from "./Auth";
import { Workspace } from "./Workspace";
import type { User } from "./types";
import "./style.css";
installDiagnostics();
function App() {
  useTranslation();
  const [state, setState] = useState<{
    phase: "loading" | "login" | "ready";
    epoch: number;
    user?: User;
    setup?: boolean;
    error?: string;
    planningZone?: string;
  }>({ phase: "loading", epoch: sessionEpoch() });
  async function auth(expected = sessionEpoch()) {
    if (!isCurrentSession(expected)) return;
    const epoch = advanceSession();
    setState({ phase: "loading", epoch });
    try {
      const m = await rootAPI("/auth/me");
      if (!isCurrentSession(epoch)) return;
      setCSRF(m.csrf);
      setDiagnosticIdentity(m.user.id);
      await changeLocale(isLocale(m.user.locale) ? m.user.locale : "ru");
      if (!isCurrentSession(epoch)) return;
      setState({
        phase: "ready",
        epoch,
        user: m.user,
        planningZone:
          typeof m.planning_timezone === "string" ? m.planning_timezone : "",
      });
    } catch {
      if (!isCurrentSession(epoch)) return;
      try {
        const s = await rootAPI("/auth/status");
        if (isCurrentSession(epoch))
          setState({ phase: "login", epoch, setup: s.setup_required });
      } catch {
        if (isCurrentSession(epoch))
          setState({ phase: "login", epoch, error: tr("md380c5043d") });
      }
    }
  }
  async function logout(expected: number) {
    if (!isCurrentSession(expected)) return;
    const epoch = advanceSession(true);
    setState({ phase: "loading", epoch });
    const pending = rootAPI("/auth/logout", "POST", {});
    setCSRF("");
    try {
      await pending;
      if (isCurrentSession(epoch))
        setState({ phase: "login", epoch, setup: false });
    } catch {
      if (isCurrentSession(epoch))
        setState({
          phase: "login",
          epoch,
          setup: false,
          error: tr("me984796ab8"),
        });
    }
  }
  useEffect(() => {
    void auth();
  }, []);
  if (state.phase === "loading")
    return (
      <>
        <LanguagePicker />
        <div className="loading">{tr("mf35bba9987")}</div>
        <BugReport key={state.epoch} view="loading" />
      </>
    );
  if (state.phase === "login")
    return (
      <React.Fragment key={state.epoch}>
        <LanguagePicker />
        {state.error && (
          <p className="error" role="alert">
            {state.error}
          </p>
        )}
        <Auth setup={!!state.setup} onReady={() => void auth(state.epoch)} />
        <BugReport view="login" />
      </React.Fragment>
    );
  return (
    <PlanningClockProvider zone={state.planningZone!}>
      <LanguagePicker authenticated />
      <Workspace
        key={state.epoch}
        user={state.user!}
        onLogout={() => void logout(state.epoch)}
      />
    </PlanningClockProvider>
  );
}
createRoot(document.getElementById("root")!).render(<App />);
