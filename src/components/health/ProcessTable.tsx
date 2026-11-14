import { useId } from "react";

import type { ProcessUsage } from "../../mygo";
import { formatBytes } from "../common/format";
import { formatProcessCPU } from "./format";

interface ProcessTableProps {
  title: string;
  /** null when the processes could not be read, undefined before the first sample. */
  processes: ProcessUsage[] | null | undefined;
  /** The column the list is sorted by, which is emphasised. */
  sortedBy: "cpu" | "memory";
}

/** ProcessTable is one of the two top-process lists: name with PID, CPU and memory. It only shows: Spruce never ends processes. */
export function ProcessTable({ title, processes, sortedBy }: ProcessTableProps) {
  const titleId = useId();
  return (
    <section aria-labelledby={titleId} className="process-card">
      <h3 id={titleId} className="process-card-title">
        {title}
      </h3>
      {processes === undefined ? (
        <span className="process-card-note">正在读取…</span>
      ) : processes === null ? (
        <span className="process-card-note">无法读取</span>
      ) : processes.length === 0 ? (
        <span className="process-card-note">正在测量…</span>
      ) : (
        <table className="process-table" aria-labelledby={titleId}>
          <thead>
            <tr>
              <th scope="col">进程</th>
              <th scope="col" className={sortedBy === "cpu" ? "process-num is-sorted" : "process-num"}>
                CPU
              </th>
              <th scope="col" className={sortedBy === "memory" ? "process-num is-sorted" : "process-num"}>
                内存
              </th>
            </tr>
          </thead>
          <tbody>
            {processes.map((p) => (
              <tr key={p.pid}>
                <th scope="row" aria-label={`${p.name}，PID ${p.pid}`}>
                  <span className="process-name">
                    <span className="process-name-text" title={p.name}>
                      {p.name}
                    </span>
                    <span className="process-pid">{`PID ${p.pid}`}</span>
                  </span>
                </th>
                <td className="process-num">
                  {p.cpuPercent === null ? "—" : formatProcessCPU(p.cpuPercent)}
                </td>
                <td className="process-num">{p.memory === null ? "—" : formatBytes(p.memory)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
