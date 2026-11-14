import { useMutation } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { events, Health, type Alert } from "../../mygo";
import { systemMonitorName } from "../../platform";
import { formatAlertTitle } from "./summary";

/** useHealthAlerts returns the current CPU alerts: the list Health.alerts returns when the page opens, then every list the Go side broadcasts. The subscription starts first, and a broadcast always wins over the initial answer, which may be older. */
export function useHealthAlerts(): Alert[] {
  const [alerts, setAlerts] = useState<Alert[]>([]);
  useEffect(() => {
    let current = true;
    let broadcast = false;
    const off = events.healthAlerts.on((next) => {
      broadcast = true;
      setAlerts(next);
    });
    void Health.alerts().then((initial) => {
      if (current && !broadcast) setAlerts(initial);
    });
    return () => {
      current = false;
      off();
    };
  }, []);
  return alerts;
}

/** AlertBanner warns about a process that has kept the CPU busy for a long time. It only informs: Spruce never ends processes. The banner is absent while there is no alert; its live region stays so screen readers hear it appear. */
export function AlertBanner() {
  const alerts = useHealthAlerts();
  const openMonitor = useMutation({ mutationFn: () => Health.openSystemMonitor() });
  const top = alerts[0];
  const monitor = systemMonitorName();
  const others = alerts.length - 1;

  return (
    <div aria-live="polite" className="alert-banner-region">
      {top && (
        <section aria-label="CPU 占用提醒" className="alert-banner">
          <span aria-hidden="true" className="alert-banner-icon">
            <svg
              width="22"
              height="22"
              viewBox="0 0 24 24"
              fill="none"
              stroke="#FFD27A"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M12 4l9 16H3z" />
              <path d="M12 10v4M12 17v.5" />
            </svg>
          </span>
          <div className="alert-banner-text">
            <span className="alert-banner-title">{formatAlertTitle(top)}</span>
            <span className="alert-banner-sub">
              {others > 0 && `另有 ${others} 个进程也在高占用。`}
              {monitor.unit}
              {`如果不是你正在用它，可以在${monitor.name}里看看。Spruce 不会结束进程，只提醒你。`}
            </span>
            {openMonitor.isError && (
              <span role="alert" className="alert-banner-error">
                {`无法打开${monitor.name}`}
              </span>
            )}
          </div>
          <button type="button" className="alert-banner-button" onClick={() => openMonitor.mutate()}>
            查看详情
          </button>
        </section>
      )}
    </div>
  );
}
