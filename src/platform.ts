import { isMyGo, runtime } from "mygo-runtime";

/** isWindows reports whether the page runs in the Windows build of the app. Outside a MyGo window (the dev server in a browser, tests) it is false, so the macOS wording is the default. */
export function isWindows(): boolean {
  return isMyGo() && runtime().platform === "win32";
}

/** The system's process monitor, as 查看详情 opens it (Health.openSystemMonitor). */
export interface SystemMonitor {
  name: string;
  /** A note on the CPU unit where the monitor counts differently from the alert, or "" where it matches. */
  unit: string;
}

/** systemMonitorName names the process monitor of this platform. The alert's CPU percent is the share of one logical CPU, as Activity Monitor shows it; Task Manager shows the share of the whole machine, so on Windows the banner says which unit it uses. */
export function systemMonitorName(): SystemMonitor {
  if (isWindows()) {
    return { name: "任务管理器", unit: "占用按单个处理器核心计算，任务管理器按全部核心显示，数值会更低。" };
  }
  return { name: "活动监视器", unit: "" };
}
