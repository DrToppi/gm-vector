import ru from "../src/locales/ru.json" with { type: "json" };
import {
  test,
  expect,
  step,
  api,
  payload,
  click,
  field,
  enroll,
  login,
  apply,
  selectProject,
  downloaded,
  installGate,
  releaseGate,
  nativeInteractions,
  ownedContext,
  otp,
  validateNegativeContracts,
  decryptBug,
  wrongReportKey,
  certificateFailure,
  proveServer,
} from "./fixtures";
import type { BrowserContext } from "@playwright/test";

test("critical real workspace B01-B09", async ({
  page,
  fixture: f,
  browser,
  context,
}, info) => {
  let admin!: Awaited<ReturnType<typeof enroll>>,
    member!: Awaited<ReturnType<typeof enroll>>,
    memberID = "",
    projectID = "",
    childID = "";
  const extra: BrowserContext[] = [];
  const memberName = "pw-invited",
    memberPassword = f.password + "-member";
  try {
    await step(info, "B01", async () => {
      await page.goto(f.origin);
      await expect(page).toHaveTitle("gmVector");
      await expect(
        page.locator('input[autocomplete="username"]'),
      ).toBeVisible();
      await field(page, 'input[autocomplete="username"]', f.username);
      admin = await enroll(page, f.password, "Synthetic administrator");
      expect((await api(page, "/auth/me")).user.admin).toBe(true);
      const invitation = await api(page, "/users", "POST", {
        username: memberName,
        name: "Synthetic invited member",
        admin: false,
      });
      memberID = invitation.id;
      await click(page, "Выйти");
      await expect(
        page.locator('input[autocomplete="username"]'),
      ).toBeVisible();
      await page.goto("about:blank");
      await page.goto(
        f.origin +
          "/#setup=" +
          encodeURIComponent(invitation.enrollment_token) +
          "&login=" +
          memberName,
      );
      await expect(page.locator('input[autocomplete="username"]')).toHaveValue(
        memberName,
      );
      await expect.poll(() => page.evaluate(() => location.hash)).toBe("");
      member = await enroll(page, memberPassword, "Synthetic invited member");
      expect((await api(page, "/auth/me")).user.id).toBe(memberID);
      const denied = await page.evaluate(
        async () => (await fetch("/api/v1/users")).status,
      );
      expect(denied).toBe(403);
      await click(page, "Выйти");
      const code = otp(admin.secret);
      await login(page, f.username, f.password, code);
      const replay = await context.request.post(
        f.origin + "/api/v1/auth/login",
        {
          headers: { Origin: f.origin },
          data: { username: f.username, password: f.password, code },
        },
      );
      expect(replay.status()).toBe(401);
      expect((await api(page, "/auth/me")).user.username).toBe(f.username);
    });
    await step(info, "B02", async () => {
      await click(page, "＋ Новый проект");
      await field(page, "dialog form input", "E2E synthetic project");
      await field(page, 'dialog input[type="date"]', "2026-10-23");
      await page.locator("dialog select").selectOption(f.calendar_id);
      await click(page, "Создать");
      await expect(page.locator(".project-header h1")).toHaveText(
        "E2E synthetic project",
      );
      projectID = (await api(page, "/projects")).find(
        (p: any) => p.title === "E2E synthetic project",
      ).id;
      await page.locator(".project-actions:not(.export-menu) summary").click();
      await click(page, "＋ Задача");
      await field(page, "dialog form input", "PW milestone");
      await field(page, 'dialog input[max="1000000"]', "2");
      await apply(page);
      await click(page, "Задачи");
      await page
        .getByRole("button", {
          name: "Добавить подзадачу: PW milestone",
          exact: true,
        })
        .click();
      await field(page, "dialog form input", "PW child");
      await field(page, 'dialog input[max="1000000"]', "16");
      await field(page, "dialog input[data-duration-days]", "2");
      await apply(page);
      let p = (await api(page, "/projects/" + projectID)).project;
      const parent = p.tasks.find((t: any) => !t.parent),
        child = p.tasks.find((t: any) => t.parent);
      childID = child.id;
      expect(parent.summary).toBe(true);
      expect(parent.planned_minutes).toBe(960);
      const employee = await api(page, "/employees", "POST", {
        id: "",
        name: "PW worker",
        daily_minutes: 480,
        user_id: "",
        provider: "",
        external_id: "",
      });
      const activity = {
        active: true,
        start: "09:00",
        end: "13:00",
        daily_minutes: 240,
      };
      p.people = [
        {
          employee_id: employee.id,
          weekdays: [],
          window_start: "",
          window_end: "",
          exceptions: {},
          week: Object.fromEntries(
            [1, 2, 3, 4, 5].map((d) => [String(d), activity]),
          ),
        },
      ];
      p.tasks.find((t: any) => t.id === childID).assignments = [
        { employee_id: employee.id, daily_minutes: 240 },
      ];
      p = await api(page, "/projects/" + projectID, "PUT", {
        project: payload(p),
        override_reason: "",
      });
      const scheduled = p.tasks.find((t: any) => t.id === childID);
      expect(scheduled.planned_minutes).toBe(960);
      expect(scheduled.start).toBe("2026-10-23");
      expect(scheduled.end).toBe("2026-10-28");
      await page.reload();
      await click(page, "Задачи");
      await click(page, "PW child");
      await field(page, "dialog form input", "Unsaved cancelled title");
      await page.locator("dialog header button").click();
      expect(
        (await api(page, "/projects/" + projectID)).project.tasks.find(
          (t: any) => t.id === childID,
        ).title,
      ).toBe("PW child");
      await click(page, "Сотрудники");
      await expect(
        page.locator('[data-participant="' + employee.id + '"]'),
      ).toContainText("09:00–13:00");
    });
    await step(info, "B03", async () => {
      const other = await api(page, "/projects", "POST", {
        title: "PW other project",
        start: "2026-10-23",
        calendar_id: f.calendar_id,
        duration_mode: "working",
      });
      await page.reload();
      await selectProject(page, "E2E synthetic project");
      await installGate(page, "/projects/" + projectID);
      await page
        .locator("nav button")
        .filter({ hasText: "E2E synthetic project" })
        .first()
        .click();
      await page.waitForFunction(() => (window as any).__pwGate.ready);
      await selectProject(page, "PW other project");
      let p = (await api(page, "/projects/" + projectID)).project;
      p.title = "PW newest A";
      await api(page, "/projects/" + projectID, "PUT", {
        project: payload(p),
        override_reason: "",
      });
      await page
        .locator("nav button")
        .filter({ hasText: "E2E synthetic project" })
        .first()
        .click();
      await expect(page.locator(".project-header h1")).toHaveText(
        "PW newest A",
      );
      await releaseGate(page);
      await expect(page.locator(".project-header h1")).toHaveText(
        "PW newest A",
      );
      await click(page, "Задачи");
      await click(page, "PW child");
      await field(page, "dialog form input", "PW saved child");
      await click(page, "Проверить и сохранить");
      await expect(page.locator("dialog h2")).toHaveText("Проверка изменений");
      await installGate(page, "/projects/" + projectID, "PUT");
      await click(page, "Применить изменения");
      await page.waitForFunction(() => (window as any).__pwGate.ready);
      // The actual write is committed, but its response is held. Close the
      // still-open preview using its real control before changing selection.
      await page.locator("dialog header button").click();
      await expect(page.locator("dialog")).toHaveCount(0);
      await selectProject(page, "PW other project");
      await releaseGate(page);
      await expect(page.locator(".project-header h1")).toHaveText(
        "PW other project",
      );
      expect(
        (await api(page, "/projects/" + projectID)).project.tasks.find(
          (t: any) => t.id === childID,
        ).title,
      ).toBe("PW saved child");
      await api(page, "/projects/" + projectID + "/members", "PUT", {
        user_id: memberID,
        role: "member",
      });
      p = (await api(page, "/projects/" + projectID)).project;
      p.title = "E2E synthetic project";
      p.tasks.find((t: any) => t.id === childID).title = "PW child";
      await api(page, "/projects/" + projectID, "PUT", {
        project: payload(p),
        override_reason: "",
      });
      const revision = (await api(page, "/projects/" + projectID)).project
        .revision;
      const csrfDenied = await page.evaluate(
        async ({ projectID, project }) =>
          (
            await fetch("/api/v1/projects/" + projectID + "/preview", {
              method: "POST",
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify({ project, override_reason: "" }),
            })
          ).status,
        { projectID, project: payload(p) },
      );
      expect(csrfDenied).toBe(403);
      expect((await api(page, "/projects/" + projectID)).project.revision).toBe(
        revision,
      );
      await page.reload();
      await selectProject(page, "E2E synthetic project");
      await installGate(page, "/projects/" + projectID);
      await page
        .locator("nav button")
        .filter({ hasText: "E2E synthetic project" })
        .first()
        .click();
      await page.waitForFunction(() => (window as any).__pwGate.ready);
      await click(page, "Выйти");
      await login(page, memberName, memberPassword, member.recovery.shift()!);
      await expect(page.locator(".project-header h1")).toHaveText(
        "E2E synthetic project",
      );
      await releaseGate(page);
      await expect(
        page.getByRole("button", { name: "Настройки проекта", exact: true }),
      ).toHaveCount(0);
      expect((await api(page, "/auth/me")).user.id).toBe(memberID);
      await click(page, "Выйти");
      await login(page, f.username, f.password, admin.recovery.shift()!);
      expect((await api(page, "/projects/" + other.id)).project.id).toBe(
        other.id,
      );
    });
    await step(info, "B04", async () => {
      await selectProject(page, "PW retained history");
      await click(page, "Задачи");
      await click(page, "History root");
      await expect(page.locator("[data-history-totals]")).toContainText("1002");
      await expect(page.locator("[data-history-legacy]")).toContainText("0.67");
      await expect(page.locator(".task-history [data-time-entry]")).toHaveCount(
        100,
      );
      await expect(
        page.locator('[data-time-entry="pw-entry-0001"]'),
      ).toContainText(ru["history.unknownEmployee"]);
      await page.locator("[data-history-next]").click();
      await expect(page.locator(".task-history [data-time-entry]")).toHaveCount(
        100,
      );
      await field(page, "dialog form input", "Retained history draft");
      const p = (await api(page, "/projects/" + f.history_project)).project;
      p.brief = "Changed history revision";
      await api(page, "/projects/" + f.history_project, "PUT", {
        project: payload(p),
        override_reason: "",
      });
      await page.locator("[data-history-next]").click();
      await expect(page.locator(".task-history .error")).toBeVisible();
      await expect(page.locator("dialog form input").first()).toHaveValue(
        "Retained history draft",
      );
      await click(page, "Загрузить первую страницу");
      await expect(page.locator("[data-history-totals]")).toContainText("1002");
      await page
        .locator('[data-history-filter="employee_id"]')
        .selectOption("history-employee");
      await click(page, "Применить фильтры");
      await expect(page.locator("[data-history-totals]")).toContainText("1001");
      await field(page, '[data-history-filter="date_from"]', "2025-01-01");
      await click(page, "Применить фильтры");
      await expect(page.locator(".task-history [data-time-entry]")).toHaveCount(
        0,
      );
      await expect(page.locator("[data-history-legacy]")).toContainText("0.67");
      await click(page, "Сбросить фильтры");
      await expect(page.locator("[data-history-totals]")).toContainText("1002");
      await page.locator(".task-history .history-export summary").click();
      for (const format of ["json", "csv", "html"]) {
        const text = await downloaded(page, () =>
          page
            .locator('.task-history [data-history-export="' + format + '"]')
            .click(),
        );
        if (format === "json") {
          const doc = JSON.parse(text);
          expect(doc.items).toHaveLength(1002);
          expect(doc.legacy_unattributed.minutes).toBe(40);
          expect(
            doc.items.find((entry: any) => entry.id === "pw-entry-0001")
              .employee_id,
          ).toBe("");
          expect(
            new Set(doc.items.map((entry: any) => entry.task_id)).size,
          ).toBe(2);
        } else
          expect(text.match(/Synthetic retained fact/g)).toHaveLength(1002);
      }
      await page.locator("dialog header button").click();
      await selectProject(page, "E2E synthetic project");
      await page.locator(".project-actions.export-menu summary").click();
      const portable = JSON.parse(
        await downloaded(page, () =>
          page.locator('[data-export-format="portable"]').click(),
        ),
      );
      expect(portable.metadata.planning_timezone).toBe("America/New_York");
      portable.metadata.planning_timezone = "UTC";
      portable.project.title = "PW portable copy";
      const start = portable.project.start;
      await click(page, "＋ Новый проект");
      await page
        .locator('.new-project-import input[type="file"]')
        .setInputFiles({
          name: "synthetic-portable.json",
          mimeType: "application/json",
          buffer: Buffer.from(JSON.stringify(portable)),
        });
      await expect(page.locator(".new-project-import .error")).toContainText(
        "обычный импорт не создаёт людей",
      );
      await expect(page.locator("[data-timezone-ack]")).toHaveCount(0);
      portable.project.people = [];
      for (const task of portable.project.tasks) task.assignments = [];
      await page
        .locator('.new-project-import input[type="file"]')
        .setInputFiles({
          name: "synthetic-portable-template.json",
          mimeType: "application/json",
          buffer: Buffer.from(JSON.stringify(portable)),
        });
      try {
        await expect(page.locator("[data-timezone-ack]")).toBeVisible();
      } catch (cause) {
        if (process.env.GMA_PW_PRIVATE_DEBUG === "1")
          console.error(
            "PW_PRIVATE_ERROR " +
              JSON.stringify({
                message:
                  "PW_PORTABLE_IMPORT_UI " +
                  (
                    await page
                      .locator(".new-project-import .error")
                      .allTextContents()
                  )
                    .join(" ")
                    .slice(0, 4096),
              }),
          );
        throw cause;
      }
      await expect(
        page.locator(".new-project-import .toolbar button").first(),
      ).toBeDisabled();
      await page.locator("[data-timezone-ack]").check();
      await page.locator(".new-project-import .toolbar button").first().click();
      await expect(page.locator(".project-header h1")).toHaveText(
        "PW portable copy",
      );
      const copy = (await api(page, "/projects")).find(
        (x: any) => x.title === "PW portable copy",
      );
      const imported = (await api(page, "/projects/" + copy.id)).project;
      expect(imported.start).toBe(start);
      expect(imported.people).toHaveLength(0);
      expect(
        imported.tasks.every((task: any) => !task.assignments.length),
      ).toBe(true);
      expect(
        imported.tasks.find((task: any) => task.id === childID).requested_start,
      ).toBe("2026-10-23");
      await selectProject(page, "E2E synthetic project");
    });
    await step(info, "B05", async () => {
      const p = (await api(page, "/projects/" + projectID)).project;
      await nativeInteractions(
        page,
        projectID,
        p.tasks.find((t: any) => !t.summary),
      );
      await click(page, "Гант");
      const handle = page.locator(
        '[data-id="' + childID + '"][data-resize="out"]',
      );
      const box = await handle.boundingBox();
      if (!box) throw Error("PW_RESIZE_HANDLE");
      const before = (await api(page, "/projects/" + projectID)).project;
      await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
      await page.mouse.down();
      await page.mouse.move(
        box.x + box.width / 2 + 32,
        box.y + box.height / 2,
        { steps: 4 },
      );
      await page.mouse.up();
      await expect
        .poll(
          async () =>
            (await api(page, "/projects/" + projectID)).project.revision,
        )
        .toBeGreaterThan(before.revision);
      expect(
        (await api(page, "/projects/" + projectID)).project.tasks.find(
          (t: any) => t.id === childID,
        ).planned_minutes,
      ).toBe(960);
      const resized = (
        await api(page, "/projects/" + projectID)
      ).project.tasks.find((task: any) => task.id === childID);
      expect(resized.requested_start).toBe("2026-10-23");
      expect(resized.duration_days).toBe(5);
      expect(resized.start).toBe("2026-10-23");
      expect(resized.end).toBe("2026-10-29");
      const tracedProject = (await api(page, "/projects/" + projectID)).project;
      const traceTask = (id: string, title: string) => ({
        ...resized,
        id,
        title,
        parent: "",
        responsibility: "customer",
        customer_owner: "",
        customer_due: "",
        customer_email: "",
        requested_start: "2026-10-23",
        start: "2026-10-23",
        end: "2026-10-23",
        duration_days: 1,
        planned_minutes: 0,
        actual_minutes: 0,
        summary: false,
        status: id === "pw-predecessor" ? "done" : "todo",
        assignments: [],
      });
      tracedProject.tasks.push(
        traceTask("pw-predecessor", "PW predecessor"),
        traceTask("pw-successor", "PW successor"),
        traceTask("pw-unrelated", "PW unrelated"),
      );
      tracedProject.links = [
        { from: "pw-predecessor", to: childID, mode: "together", days: 0 },
        { from: childID, to: "pw-successor", mode: "after", days: 0 },
      ];
      await api(page, "/projects/" + projectID, "PUT", {
        project: payload(tracedProject),
        override_reason: "",
      });
      await page.reload();
      await click(page, "Майнд-мэп");
      await click(page, "Flow");
      const camera = await page
        .locator("[data-map-content]")
        .getAttribute("transform");
      await page.locator('[aria-label="Trace PW child"]').click();
      await expect(page.locator('[data-map-mode="trace"]')).toBeVisible();
      for (const id of ["pw-predecessor", childID, "pw-successor"])
        await expect(page.locator('[data-task="' + id + '"]')).toHaveCount(1);
      await expect(page.locator('[data-task="pw-unrelated"]')).toHaveCount(0);
      await expect(
        page.locator('[data-from="pw-predecessor"][data-to="' + childID + '"]'),
      ).toHaveCount(1);
      await expect(
        page.locator('[data-from="' + childID + '"][data-to="pw-successor"]'),
      ).toHaveCount(1);
      await expect(
        page.locator('[data-map-mode="trace"] [data-link]'),
      ).toHaveCount(2);
      await click(page, "Выйти из Trace");
      await expect(page.locator("[data-map-content]")).toHaveAttribute(
        "transform",
        camera!,
      );
      const milestone = p.tasks.find((task: any) => task.summary).id;
      const beforeFold = await page
        .locator("[data-map-content]")
        .getAttribute("transform");
      await page.locator('[data-collapse="' + milestone + '"]').click();
      await expect(page.locator('[data-task="' + childID + '"]')).toHaveCount(
        0,
      );
      const folded = await page
        .locator("[data-map-content]")
        .getAttribute("transform");
      expect(folded!.match(/scale\(([^)]+)\)/)![1]).toBe(
        beforeFold!.match(/scale\(([^)]+)\)/)![1],
      );
      await page.locator('[data-collapse="' + milestone + '"]').click();
      await expect(page.locator('[data-task="' + childID + '"]')).toHaveCount(
        1,
      );
    });
    await step(info, "B06", async () => {
      const adminContext = await ownedContext(browser, f);
      extra.push(adminContext);
      const adminPage = await adminContext.newPage();
      await adminPage.goto(f.origin);
      await login(adminPage, f.username, f.password, admin.recovery.shift()!);
      expect(
        (await api(adminPage, "/projects/" + projectID)).project.tasks.find(
          (task: any) => task.id === childID,
        ).blocked,
      ).toBe(false);
      const employee = (await api(adminPage, "/employees")).find(
        (x: any) => x.name === "PW worker",
      );
      await api(adminPage, "/employees", "POST", {
        ...employee,
        user_id: memberID,
      });
      await click(page, "Выйти");
      await login(page, memberName, memberPassword, member.recovery.shift()!);
      await click(page, "Задачи");
      await click(page, "PW child");
      await expect(page.locator('dialog input[max="1000000"]')).toBeDisabled();
      await page.locator("dialog select").first().selectOption("doing");
      await page
        .locator("dialog textarea")
        .first()
        .fill("Member retained brief");
      const old = (await api(adminPage, "/projects/" + projectID)).project;
      old.brief = "Concurrent manager edit";
      await api(adminPage, "/projects/" + projectID, "PUT", {
        project: payload(old),
        override_reason: "",
      });
      await click(page, "Проверить и сохранить");
      await expect(page.locator("dialog [role=alert]").first()).toBeVisible();
      await expect(page.locator("dialog textarea").first()).toHaveValue(
        "Member retained brief",
      );
      await click(page, "Записать время");
      await expect(page.locator("[data-task-fact]")).toContainText("Факт: 1");
      await apply(page);
      const after = (await api(adminPage, "/projects/" + projectID)).project;
      const saved = after.tasks.find((t: any) => t.id === childID);
      expect(after.brief).toBe("Concurrent manager edit");
      expect(saved.status).toBe("doing");
      expect(saved.brief).toBe("Member retained brief");
      expect(saved.planned_minutes).toBe(960);
      expect(saved.actual_minutes).toBe(60);
      await click(page, "Выйти");
      await login(page, f.username, f.password, admin.recovery.shift()!);
    });
    await step(info, "B07", async () => {
      await selectProject(page, "PW retained history");
      await click(page, "Задачи");
      await click(page, "History root");
      await page.evaluate(() => {
        const original = window.open.bind(window);
        (window as any).__pwOriginalOpen = original;
        window.open = (...args: Parameters<typeof window.open>) => {
          const popup = original(...args);
          if (popup)
            popup.print = () => {
              (popup as any).__pwPrinted = true;
            };
          return popup;
        };
      });
      await page.locator(".task-history .history-export summary").click();
      const pending = page.waitForEvent("popup");
      await page.locator('[data-history-export="pdf"]').last().click();
      const popup = await pending;
      await expect(popup.locator("tbody tr")).toHaveCount(1002);
      await popup.waitForFunction(() => (window as any).__pwPrinted === true);
      await downloaded(page, () =>
        page.locator('.task-history [data-history-export="csv"]').click(),
      );
      await page.locator("dialog header button").click();
      await expect.poll(() => popup.isClosed()).toBe(true);
      await click(page, "History root");
      await page.locator(".task-history .history-export summary").click();
      await page.evaluate(() => {
        window.open = () => null;
      });
      await page.locator('.task-history [data-history-export="pdf"]').click();
      await expect(
        page.locator(".task-history .history-export .error"),
      ).toBeVisible();
      await page.evaluate(() => {
        window.open = (window as any).__pwOriginalOpen;
      });
      const failedPopup = page.waitForEvent("popup");
      await page.route(
        "**/time-history/export?**",
        (route) =>
          route.fulfill({
            status: 503,
            contentType: "application/json",
            body: JSON.stringify({
              error: {
                code: "unavailable",
                message: "Synthetic export failure",
              },
            }),
          }),
        { times: 1 },
      );
      await page.locator('.task-history [data-history-export="pdf"]').click();
      const failed = await failedPopup;
      await expect.poll(() => failed.isClosed()).toBe(true);
      await page.locator("dialog header button").click();
      await page.locator('[data-bug-report="open"]').click();
      for (const name of ["title", "steps", "expected", "actual"])
        await page
          .locator('[data-bug-report-field="' + name + '"]')
          .fill("Synthetic " + name);
      await page.locator('[data-bug-report="issue-preview"]').click();
      const nativeClipboard = await page.evaluate(() => ({
        secure: isSecureContext,
        available: typeof navigator.clipboard?.writeText === "function",
      }));
      expect(nativeClipboard.secure).toBe(true);
      await page.locator('[data-bug-report="issue-copy"]').click();
      await expect
        .poll(async () =>
          [ru.issueCopied, ru.issueCopyError].includes(
            (await page.locator(".public-issue [role=status]").textContent()) ||
              "",
          ),
        )
        .toBe(true);
      const nativeStatus = await page
        .locator(".public-issue [role=status]")
        .textContent();
      const nativeOutcome = !nativeClipboard.available
        ? "api-unavailable"
        : nativeStatus === ru.issueCopied
          ? "write-succeeded"
          : "write-denied";
      if (!nativeClipboard.available)
        expect(nativeStatus).toBe(ru.issueCopyError);
      info.annotations.push({
        type: "pw-native-clipboard",
        description: nativeOutcome,
      });
      await page.evaluate(() => {
        (window as any).__pwClipboardCounts = { success: 0, denied: 0 };
        Object.defineProperty(navigator, "clipboard", {
          configurable: true,
          value: {
            writeText: async () => {
              (window as any).__pwClipboardCounts.success++;
            },
          },
        });
      });
      await page.locator('[data-bug-report="issue-copy"]').click();
      await expect(page.locator(".public-issue [role=status]")).toHaveText(
        ru.issueCopied,
      );
      expect(
        await page.evaluate(() => (window as any).__pwClipboardCounts.success),
      ).toBe(1);
      await page.evaluate(() =>
        Object.defineProperty(navigator, "clipboard", {
          configurable: true,
          value: {
            writeText: async () => {
              (window as any).__pwClipboardCounts.denied++;
              throw Error("Synthetic denied");
            },
          },
        }),
      );
      await page.locator('[data-bug-report="issue-copy"]').click();
      await expect(page.locator(".public-issue [role=status]")).toHaveText(
        ru.issueCopyError,
      );
      expect(
        await page.evaluate(() => (window as any).__pwClipboardCounts.denied),
      ).toBe(1);
      info.annotations.push({
        type: "pw-clipboard-ui",
        description: "controlled-success-and-denial",
      });
    });
    await step(info, "B08", async () => {
      const marker = "PW_PRIVATE_" + f.nonce;
      await page.evaluate((marker) => {
        history.replaceState(
          null,
          "",
          "/?private=" + marker + "#fragment=" + marker,
        );
        console.error(marker);
      }, marker);
      await page.locator('[data-bug-report="prepare"]').click();
      await expect(page.locator('[data-bug-report="preview"]')).toBeVisible();
      const report = JSON.parse(
        (await page.locator('[data-bug-report="preview"]').textContent())!,
      );
      for (const secret of [marker, f.origin, f.password, admin.secret])
        expect(JSON.stringify(report)).not.toContain(secret);
      await page.locator('[data-bug-report="issue-preview"]').click();
      const publicText = (await page
        .locator('[data-bug-report="issue-body"]')
        .textContent())!;
      expect(publicText).toContain("Synthetic steps");
      for (const secret of [
        marker,
        f.origin,
        f.password,
        admin.secret,
        "request_id",
        "server.events",
      ])
        expect(publicText).not.toContain(secret);
      await page.locator('[data-bug-report="reviewed"]').check();
      const encrypted = await downloaded(page, () =>
        page.locator('[data-bug-report="download"]').click(),
      );
      expect(encrypted).not.toContain("Synthetic title");
      expect(decryptBug(encrypted, f.report_private_key)).toEqual(report);
      expect(() => decryptBug(encrypted, wrongReportKey())).toThrow();
      await expect(page.locator('[data-bug-report="issue-open"]')).toHaveCount(
        0,
      );
      await page.locator('[data-bug-report="issue-reviewed"]').check();
      await expect(
        page.locator('[data-bug-report="issue-open"]'),
      ).toHaveAttribute("rel", "noopener noreferrer");
      const blocked = context.waitForEvent("requestfailed", {
        predicate: (request) => request.url().startsWith("https://github.com/"),
      });
      await page.locator('[data-bug-report="issue-open"]').click();
      await blocked;
      await page.locator("dialog header button").click();
      await page.evaluate(() => history.replaceState(null, "", "/"));
    });
    await step(info, "B09", async () => {
      validateNegativeContracts(f);
      const strict = await browser.newContext({ ignoreHTTPSErrors: false });
      extra.push(strict);
      for (const target of [f.origin, f.mismatch_origin]) {
        await proveServer(f, target === f.mismatch_origin);
        const probe = await strict.newPage();
        let classification: string | null = null;
        try {
          await probe.goto(target);
        } catch (cause) {
          classification = certificateFailure(
            f.engine,
            (cause as Error).message,
          );
        }
        expect(classification).toBe(f.engine + "-certificate-rejected");
        info.annotations.push({
          type: "pw-certificate-rejection",
          description: classification!,
        });
        await probe.close();
      }
      expect(info.retry).toBe(0);
      expect(info.project.name).toBe(f.engine);
    });
  } finally {
    for (const owned of extra) await owned.close();
  }
});
