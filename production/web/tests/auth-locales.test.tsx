import {
  act,
  fireEvent,
  render,
  renderHook,
  screen,
} from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useTranslation } from "react-i18next";
import { Auth } from "../src/Auth";
import { advanceSession, setCSRF } from "../src/api";
import { useSessionScope } from "../src/useSessionScope";
import { changeLocale, tr, type Locale } from "../src/i18n";
import { reply, scenario } from "./fixtures";

describe("initial access lifecycle and translated rendering", () => {
  const current = scenario();

  it("resumes the initial invitation once and never resumes merely because the token was edited", async () => {
    const f = current();
    f.hold("POST", "/auth/bootstrap/resume");
    history.replaceState(null, "", "/#setup=synthetic-invitation");
    const view = render(<Auth setup={false} onReady={vi.fn()} />);
    expect(f.find("POST", "/auth/bootstrap/resume")).toHaveLength(1);
    expect(f.latest("POST", "/auth/bootstrap/resume").body).toEqual({
      token: "synthetic-invitation",
    });
    await reply(
      f.latest("POST", "/auth/bootstrap/resume"),
      {
        error: {
          code: "validation",
          message: "Synthetic incomplete enrollment",
        },
      },
      422,
    );
    const token = view.container.querySelector(
      'input[autocomplete="off"]',
    ) as HTMLInputElement;
    expect(token.value).toBe("synthetic-invitation");
    fireEvent.change(token, { target: { value: "edited-invitation" } });
    view.rerender(<Auth setup={false} onReady={vi.fn()} />);
    expect(f.find("POST", "/auth/bootstrap/resume")).toHaveLength(1);
    expect(token.value).toBe("edited-invitation");
  });

  it("does not auto-resume the enrollment token received by manual first setup", async () => {
    const f = current();
    f.hold("POST", "/auth/bootstrap");
    const ready = vi.fn();
    const view = render(<Auth setup onReady={ready} />);
    expect(f.requests).toHaveLength(0);
    fireEvent.change(screen.getByLabelText(tr("m5be12bfe49")), {
      target: { value: "component-admin" },
    });
    fireEvent.change(screen.getByLabelText(tr("m1da7e9372e")), {
      target: { value: "Example Administrator" },
    });
    fireEvent.change(screen.getByLabelText(tr("mcb1a2074b3")), {
      target: { value: "synthetic component password" },
    });
    fireEvent.submit(view.container.querySelector("form")!);
    expect(
      f.latest("POST", "/auth/bootstrap").headers.get("X-CSRF-Token"),
    ).toBe("synthetic-csrf");
    await reply(f.latest("POST", "/auth/bootstrap"), {
      enrollment_token: "synthetic-created-enrollment",
      secret: "SYNTHETIC-MANUAL-KEY",
      otpauth_uri: "otpauth://totp/Example?secret=SYNTHETIC",
    });
    expect(screen.getByText("SYNTHETIC-MANUAL-KEY")).toBeTruthy();
    expect(f.find("POST", "/auth/bootstrap/resume")).toHaveLength(0);
    expect(f.requests).toHaveLength(1);
    expect(ready).not.toHaveBeenCalled();
    expect(
      screen.queryByLabelText(tr("mcb1a2074b3")) as HTMLInputElement | null,
    ).toBeNull();
  });

  it("rejects a body arriving after session replacement and never adopts the new credentials in an old callback", async () => {
    const f = current();
    f.hold("GET", "/projects/A");
    const oldScope = renderHook(() => useSessionScope());
    const obsolete = oldScope.result.current.api;
    const result = obsolete("/projects/A").then(
      () => ({ name: "unexpected-success" }),
      (error: Error) => error,
    );
    const pending = f.latest("GET", "/projects/A");
    await act(async () => pending.replyHeaders());
    await act(async () => {
      advanceSession();
      setCSRF("synthetic-new-session");
    });
    expect(pending.signal?.aborted).toBe(true);
    await act(async () => pending.replyBody({ title: "Old session response" }));
    expect((await result).name).toBe("AbortError");
    await expect(obsolete("/projects/A")).rejects.toMatchObject({
      name: "AbortError",
    });
    expect(f.find("GET", "/projects/A")).toHaveLength(1);
    oldScope.unmount();
    const freshScope = renderHook(() => useSessionScope());
    const fresh = freshScope.result.current.api<{ title: string }>(
      "/projects/A",
    );
    expect(f.latest("GET", "/projects/A").headers.get("X-CSRF-Token")).toBe(
      "synthetic-new-session",
    );
    await reply(f.latest("GET", "/projects/A"), {
      title: "New session response",
    });
    expect(await fresh).toEqual({ title: "New session response" });
    freshScope.unmount();
  });

  const labels: Record<Locale, string> = {
    en: "Username",
    ru: "Логин",
    de: "Benutzername",
    fr: "Identifiant",
    es: "Nombre de usuario",
    "pt-BR": "Usuário",
    "zh-Hans": "用户名",
  };
  it.each(Object.entries(labels) as [Locale, string][])(
    "updates the actual form in %s without issuing access requests",
    async (locale, label) => {
      const f = current();
      function TranslatedAccess() {
        useTranslation();
        return <Auth setup={false} onReady={vi.fn()} />;
      }
      const view = render(<TranslatedAccess />);
      const literal =
        '<img src=x onerror="unexpected()">' + "very-long-name-".repeat(20);
      fireEvent.change(screen.getByLabelText("Username"), {
        target: { value: literal },
      });
      await act(async () => changeLocale(locale));
      expect(document.documentElement.lang).toBe(locale);
      expect((screen.getByLabelText(label) as HTMLInputElement).value).toBe(
        literal,
      );
      expect(view.container.querySelector("img")).toBeNull();
      expect(f.requests).toHaveLength(0);
    },
  );
});
