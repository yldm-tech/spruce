import type { ReactNode } from "react";

import type { BatteryStats, SystemInfo, Vitals } from "../../mygo";
import { isWindows } from "../../platform";
import { formatBytes } from "../common/format";
import {
  batteryStateText,
  batteryTimeText,
  coresText,
  formatBootTime,
  formatLoad,
  formatPercent,
  formatRate,
  formatUptime,
  osText,
  pressureText,
} from "./format";
import { Sparkline, type SparklineSeries } from "./Sparkline";
import { DetailList, Meter, VitalCard } from "./VitalCard";

/** The span of every sparkline: the two minutes the sampler keeps. */
const WINDOW_SECONDS = 120;

/** The lowest top of a throughput chart's y axis (1 KB/s), so an idle disk or network draws a flat line at the bottom instead of magnifying noise. */
const MIN_RATE_TOP = 1000;

type ChartData = Parameters<typeof Sparkline>[0]["data"];

const CPU_SERIES: SparklineSeries[] = [{ stroke: "--chart-cpu", fill: "--chart-cpu-fill" }];
const MEMORY_SERIES: SparklineSeries[] = [{ stroke: "--chart-memory", fill: "--chart-memory-fill" }];
const DISK_SERIES: SparklineSeries[] = [{ stroke: "--chart-disk-read" }, { stroke: "--chart-disk-write" }];
const NETWORK_SERIES: SparklineSeries[] = [{ stroke: "--chart-net-down" }, { stroke: "--chart-net-up" }];

/** The value a VitalCard shows for a metric: null before the first sample, false when it could not be read. */
function state<T>(latest: Vitals | undefined, metric: T | null | undefined): T | null | false {
  if (!latest) return null;
  return metric ?? false;
}

function usageLevel(percent: number): "normal" | "warning" | "critical" {
  if (percent >= 95) return "critical";
  if (percent >= 85) return "warning";
  return "normal";
}

function Legend({ items }: { items: readonly [token: string, text: string][] }) {
  return (
    <span className="chart-legend">
      {items.map(([token, text]) => (
        <span key={text} className="chart-legend-item">
          <span aria-hidden="true" className="chart-legend-swatch" style={{ background: `var(${token})` }} />
          {text}
        </span>
      ))}
    </span>
  );
}

export function CPUCard({ latest, info, chart }: { latest?: Vitals; info?: SystemInfo; chart: ChartData }) {
  const cpu = state(latest, latest?.cpu);
  const cores = info ? coresText(info) : "";
  return (
    <VitalCard
      title="CPU"
      tone="amber"
      icon={
        <>
          <rect x="6" y="6" width="12" height="12" rx="2" />
          <path d="M9 2v4M15 2v4M9 18v4M15 18v4M2 9h4M2 15h4M18 9h4M18 15h4" />
        </>
      }
      value={cpu && formatPercent(cpu.percent)}
      caption={cpu && cpu.load ? `负载 ${formatLoad(cpu.load)}` : undefined}
    >
      {cpu && (
        <>
          <div
            role="img"
            aria-label={`各核心占用：${cpu.perCore.map((p) => formatPercent(p)).join("、")}`}
            className="core-bars"
          >
            {cpu.perCore.map((p, i) => (
              // Cores have no identity but their position.
              <span key={i} className="core-bar">
                <span className="core-bar-fill" style={{ height: `${Math.min(100, Math.max(0, p))}%` }} />
              </span>
            ))}
          </div>
          <Sparkline data={chart} series={CPU_SERIES} windowSeconds={WINDOW_SECONDS} max={100} />
          {(info?.cpuModel || cores) && (
            <span className="vital-card-foot">{[info?.cpuModel, cores].filter(Boolean).join(" · ")}</span>
          )}
        </>
      )}
    </VitalCard>
  );
}

export function MemoryCard({ latest, chart }: { latest?: Vitals; chart: ChartData }) {
  const memory = state(latest, latest?.memory);
  const swapLabel = isWindows() ? "页面文件" : "交换内存";
  const memoryRows: [string, ReactNode][] = [];
  if (memory) {
    memoryRows.push([swapLabel, memory.swap ? `已用 ${formatBytes(memory.swap.used)}` : "无法读取"]);
    if (memory.pressure) {
      memoryRows.push([
        "内存压力",
        <span key="pressure" className={`pressure pressure--${memory.pressure}`}>
          {pressureText(memory.pressure)}
        </span>,
      ]);
    }
  }
  return (
    <VitalCard
      title="内存"
      tone="violet"
      icon={
        <>
          <rect x="3" y="6" width="18" height="10" rx="2" />
          <path d="M7 16v3M11 16v3M15 16v3M7 10h2M11 10h2M15 10h2" />
        </>
      }
      value={memory && formatPercent(memory.percent)}
      caption={memory && `已用 ${formatBytes(memory.used)}，共 ${formatBytes(memory.total)}`}
    >
      {memory && (
        <>
          <Meter
            value={memory.percent}
            label="内存已用"
            valueText={`已用 ${formatPercent(memory.percent)}`}
            level={
              memory.pressure === "critical"
                ? "critical"
                : memory.pressure === "warning"
                  ? "warning"
                  : "normal"
            }
          />
          <Sparkline data={chart} series={MEMORY_SERIES} windowSeconds={WINDOW_SECONDS} max={100} />
          <DetailList rows={memoryRows} />
        </>
      )}
    </VitalCard>
  );
}

export function DiskCard({ latest, chart }: { latest?: Vitals; chart: ChartData }) {
  const disk = state(latest, latest?.disk);
  const io = latest?.diskIo;
  const usedPercent = disk && disk.total > 0 ? (disk.used / disk.total) * 100 : 0;
  return (
    <VitalCard
      title="磁盘"
      tone="cyan"
      icon={
        <>
          <path d="M5 13l2.5-8h9L19 13" />
          <rect x="3" y="13" width="18" height="7" rx="2" />
          <path d="M7 16.5h.01M11 16.5h6" />
        </>
      }
      value={disk && `${formatBytes(disk.free)} 可用`}
      caption={disk && `已用 ${formatBytes(disk.used)}，共 ${formatBytes(disk.total)}`}
    >
      {disk && (
        <>
          <Meter
            value={usedPercent}
            label="系统磁盘已用"
            valueText={`已用 ${formatPercent(usedPercent)}`}
            level={usageLevel(usedPercent)}
          />
          <Sparkline data={chart} series={DISK_SERIES} windowSeconds={WINDOW_SECONDS} minTop={MIN_RATE_TOP} />
          {io ? (
            <Legend
              items={[
                ["--chart-disk-read", `读取 ${formatRate(io.readPerSec)}`],
                ["--chart-disk-write", `写入 ${formatRate(io.writePerSec)}`],
              ]}
            />
          ) : (
            <span className="vital-card-foot">读写速度：无法读取</span>
          )}
        </>
      )}
    </VitalCard>
  );
}

export function NetworkCard({ latest, chart }: { latest?: Vitals; chart: ChartData }) {
  const network = state(latest, latest?.network);
  return (
    <VitalCard
      title="网络"
      tone="blue"
      icon={<path d="M8 20V5M4 9l4-4 4 4M16 4v15M12 15l4 4 4-4" />}
      value={network && `↓ ${formatRate(network.downloadPerSec)}`}
      caption={network && `↑ ${formatRate(network.uploadPerSec)}`}
    >
      {network && (
        <>
          <Sparkline
            data={chart}
            series={NETWORK_SERIES}
            windowSeconds={WINDOW_SECONDS}
            minTop={MIN_RATE_TOP}
          />
          <Legend
            items={[
              ["--chart-net-down", "下载"],
              ["--chart-net-up", "上传"],
            ]}
          />
        </>
      )}
    </VitalCard>
  );
}

/** BatteryCard is shown on a machine with a battery, and with 无法读取 when the battery could not be read. */
export function BatteryCard({ battery }: { battery: BatteryStats | null }) {
  const status = battery ? batteryStateText(battery.state) : "";
  const time = battery ? batteryTimeText(battery.state, battery.minutesRemaining) : "";
  return (
    <VitalCard
      title="电池"
      tone="green"
      icon={
        <>
          <rect x="2" y="7" width="17" height="10" rx="2" />
          <path d="M22 11v2M6 10v4" />
        </>
      }
      value={battery ? formatPercent(battery.percent) : false}
      caption={[status, time].filter(Boolean).join(" · ") || undefined}
    >
      {battery && (
        <>
          <Meter
            value={battery.percent}
            label="电量"
            valueText={`电量 ${formatPercent(battery.percent)}`}
            level={battery.state === "discharging" && battery.percent <= 10 ? "critical" : "normal"}
          />
          <DetailList
            rows={[
              ["循环次数", battery.cycleCount !== undefined ? `${battery.cycleCount} 次` : "无法读取"],
              [
                "最大容量",
                battery.healthPercent !== undefined
                  ? `${formatPercent(battery.healthPercent)}（相对设计容量）`
                  : "无法读取",
              ],
            ]}
          />
        </>
      )}
    </VitalCard>
  );
}

export function SystemCard({ info, failed, now }: { info?: SystemInfo; failed: boolean; now?: number }) {
  const os = info ? osText(info) : "";
  const boot = info ? formatBootTime(info.bootTime) : "";
  const bootMs = info?.bootTime ? Date.parse(info.bootTime) : Number.NaN;
  return (
    <VitalCard
      title="系统"
      tone="slate"
      icon={
        <>
          <rect x="3" y="4" width="18" height="12" rx="2" />
          <path d="M8 20h8M12 16v4" />
        </>
      }
      value={info ? os || "无法读取" : failed ? false : null}
    >
      {info && (
        <DetailList
          rows={[
            ["版本号", info.osBuild || "无法读取"],
            ["型号", info.model || "无法读取"],
            [
              "已运行",
              !Number.isFinite(bootMs) || !boot
                ? "无法读取"
                : now === undefined
                  ? "正在读取…"
                  : formatUptime(now - bootMs),
            ],
            ["启动于", boot || "无法读取"],
          ]}
        />
      )}
    </VitalCard>
  );
}
