import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";

import type { SpaceTarget, SpaceVolume } from "../../mygo";
import { truncateMiddle } from "../common/format";
import { IconTile, type Tone } from "../common/Glyph";
import { volumeUsage } from "./labels";
import { locationsQueryOptions } from "./queries";

/** Where a scan goes: its path and what the page calls it while it runs. */
export interface ScanRequest {
  path: string;
  name: string;
}

interface StartViewProps {
  onScan: (request: ScanRequest) => void;
  /** Opens the system's folder picker. */
  onChoose: () => void;
  choosing: boolean;
  /** Why the last scan or the picker failed (a sentence to show). */
  error: string | null;
}

const DISK = (
  <>
    <rect x="3" y="6" width="18" height="12" rx="3" />
    <path d="M7 14h.01M11 14h6" />
  </>
);

const APPS = (
  <>
    <rect x="4" y="4" width="7" height="7" rx="2" />
    <rect x="13" y="4" width="7" height="7" rx="2" />
    <rect x="4" y="13" width="7" height="7" rx="2" />
    <rect x="13" y="13" width="7" height="7" rx="2" />
  </>
);

const TARGET_GLYPHS: Record<string, { tone: Tone; paths: ReactNode }> = {
  home: {
    tone: "violet",
    paths: <path d="M4 11l8-7 8 7v9h-5v-6h-6v6H4z" />,
  },
  downloads: {
    tone: "pink",
    paths: <path d="M12 4v11M7 10l5 5 5-5M5 20h14" />,
  },
  applications: {
    tone: "blue",
    paths: APPS,
  },
  programFiles: {
    tone: "blue",
    paths: APPS,
  },
};

const FOLDER = <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />;

/** StartView is the page before a scan: the disks with how full they are, the quick folders, and 选择其他文件夹. Choosing any of them starts a scan. */
export function StartView({ onScan, onChoose, choosing, error }: StartViewProps) {
  const locations = useQuery(locationsQueryOptions);
  const volumes = locations.data?.volumes ?? [];
  const targets = locations.data?.targets ?? [];
  return (
    <div className="space-start" aria-busy={locations.isPending}>
      {error && (
        <p role="alert" className="space-error">
          {error}
        </p>
      )}
      {locations.isError && (
        <p role="alert" className="space-error">
          {locations.error instanceof Error ? locations.error.message : "无法读取磁盘列表"}
        </p>
      )}
      {volumes.length > 0 && (
        <section aria-labelledby="space-volumes-title" className="space-section">
          <h2 id="space-volumes-title" className="space-section-title">
            磁盘
          </h2>
          <ul className="space-card-grid">
            {volumes.map((v) => (
              <li key={v.path}>
                <VolumeCard volume={v} onScan={onScan} />
              </li>
            ))}
          </ul>
        </section>
      )}
      {targets.length > 0 && (
        <section aria-labelledby="space-targets-title" className="space-section">
          <h2 id="space-targets-title" className="space-section-title">
            常用位置
          </h2>
          <ul className="space-card-grid">
            {targets.map((t) => (
              <li key={t.id}>
                <TargetCard target={t} onScan={onScan} />
              </li>
            ))}
          </ul>
        </section>
      )}
      <div className="space-choose">
        <button type="button" className="ghost-button" disabled={choosing} onClick={onChoose}>
          {choosing ? "正在选择…" : "选择其他文件夹…"}
        </button>
      </div>
    </div>
  );
}

function VolumeCard({ volume, onScan }: { volume: SpaceVolume; onScan: (r: ScanRequest) => void }) {
  const usage = volumeUsage(volume);
  const full = usage.share >= 0.9;
  return (
    <button
      type="button"
      className="space-card"
      onClick={() => onScan({ path: volume.path, name: volume.name })}
    >
      <IconTile tone={volume.startup ? "cyan" : "slate"} size={42} glyphSize={22} className="space-card-icon">
        {DISK}
      </IconTile>
      <span className="space-card-text">
        <span className="space-card-title">{volume.name}</span>
        <span className="space-card-sub">{usage.text}</span>
        <span aria-hidden="true" className="space-capacity">
          <span
            className={full ? "space-capacity-used space-capacity-used--full" : "space-capacity-used"}
            style={{ width: `${usage.share * 100}%` }}
          />
        </span>
        <span className="visually-hidden">{`已用 ${Math.round(usage.share * 100)}%`}</span>
      </span>
    </button>
  );
}

function TargetCard({ target, onScan }: { target: SpaceTarget; onScan: (r: ScanRequest) => void }) {
  const glyph = TARGET_GLYPHS[target.id] ?? { tone: "slate" as Tone, paths: FOLDER };
  return (
    <button
      type="button"
      className="space-card"
      title={target.path}
      onClick={() => onScan({ path: target.path, name: target.name })}
    >
      <IconTile tone={glyph.tone} size={42} glyphSize={22} className="space-card-icon">
        {glyph.paths}
      </IconTile>
      <span className="space-card-text">
        <span className="space-card-title">{target.name}</span>
        <span className="space-card-sub">{truncateMiddle(target.path, 48)}</span>
      </span>
    </button>
  );
}
