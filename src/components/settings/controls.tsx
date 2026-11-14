import { useMutation } from "@tanstack/react-query";
import { useId, useState, type ReactNode } from "react";

import { Settings, type AppSettings, type SettingRange } from "../../mygo";
import { Glyph } from "../common/Glyph";
import { Switch } from "../common/Switch";
import { errorMessage, useSetting, type FieldSpec } from "./useSetting";

/** How long a slider or a text field waits after the last change before it saves. */
export const SAVE_DEBOUNCE_MS = 400;

interface RowProps {
  label: string;
  description?: string;
  labelId: string;
  descriptionId: string;
  /** A failure, in the warning colour, announced at once. */
  error?: string | null;
  errorId?: string;
  /** An adjustment Go made, announced politely. */
  note?: string | null;
  /** Puts the control under the text, for the folder lists. */
  stacked?: boolean;
  children: ReactNode;
}

/** SettingRow is one setting in a section card: the label and its description on the left, the control on the right (or below), and the line saying a save failed or was adjusted. */
export function SettingRow({
  label,
  description,
  labelId,
  descriptionId,
  error,
  errorId,
  note,
  stacked,
  children,
}: RowProps) {
  return (
    <div className={stacked ? "setting-row setting-row--stacked" : "setting-row"}>
      <div className="setting-text">
        <span id={labelId} className="setting-label">
          {label}
        </span>
        {description && (
          <span id={descriptionId} className="setting-description">
            {description}
          </span>
        )}
        {error && (
          <span id={errorId} role="alert" className="setting-error">
            {error}
          </span>
        )}
        {note && (
          <span role="status" className="setting-note">
            {note}
          </span>
        )}
      </div>
      <div className="setting-control">{children}</div>
    </div>
  );
}

interface FieldProps<T> {
  settings: AppSettings;
  spec: FieldSpec<T>;
  label: string;
  description?: string;
}

/** useRowIds returns the ids a SettingRow and its control share. */
export function useRowIds() {
  return { labelId: useId(), descriptionId: useId(), errorId: useId() };
}

/** SwitchSetting is an on/off setting, saved as soon as it is flipped. */
export function SwitchSetting({
  settings,
  spec,
  label,
  description,
  disabled,
}: FieldProps<boolean> & { disabled?: boolean }) {
  const ids = useRowIds();
  const setting = useSetting(settings, spec);
  return (
    <SettingRow label={label} description={description} error={setting.error} note={setting.note} {...ids}>
      <Switch
        checked={setting.value}
        onChange={setting.change}
        labelledBy={ids.labelId}
        describedBy={description ? ids.descriptionId : undefined}
        disabled={disabled}
      />
    </SettingRow>
  );
}

interface SliderProps extends FieldProps<number> {
  range: SettingRange;
  step: number;
  /** The value as shown and read out, e.g. "40%" or "10 分钟". */
  format: (value: number) => string;
}

/** SliderSetting is a number on a slider over its allowed range, saved once the user stops moving it. */
export function SliderSetting({ settings, spec, label, description, range, step, format }: SliderProps) {
  const ids = useRowIds();
  const setting = useSetting(
    settings,
    { adjusted: (v) => `已调整为 ${format(v)}`, ...spec },
    SAVE_DEBOUNCE_MS,
  );
  return (
    <SettingRow label={label} description={description} error={setting.error} note={setting.note} {...ids}>
      <span className="setting-slider">
        <input
          type="range"
          min={range.min}
          max={range.max}
          step={step}
          value={setting.value}
          onChange={(e) => setting.change(Number(e.currentTarget.value))}
          onBlur={setting.flush}
          aria-labelledby={ids.labelId}
          aria-describedby={description ? ids.descriptionId : undefined}
          aria-valuetext={format(setting.value)}
        />
        <output className="setting-slider-value" aria-hidden="true">
          {format(setting.value)}
        </output>
      </span>
    </SettingRow>
  );
}

interface NumberProps extends FieldProps<number> {
  range: SettingRange;
  /** The unit after the field, e.g. "天" or "MB". */
  unit: string;
}

/** parseWhole returns the whole number text spells, or null. */
function parseWhole(text: string): number | null {
  const t = text.trim();
  if (!/^\d+$/.test(t)) return null;
  const n = Number(t);
  return Number.isSafeInteger(n) ? n : null;
}

/** NumberSetting is a whole number typed into a field. Text that is not a whole number in range shows the allowed range and is not saved; a valid number is saved once the user stops typing, or when the field loses focus. */
export function NumberSetting({ settings, spec, label, description, range, unit }: NumberProps) {
  const ids = useRowIds();
  const setting = useSetting(
    settings,
    { adjusted: (v) => `已调整为 ${v} ${unit}`, ...spec },
    SAVE_DEBOUNCE_MS,
  );
  // The text being edited; null shows the setting's value.
  const [text, setText] = useState<string | null>(null);
  const parsed = text === null ? setting.value : parseWhole(text);
  const invalid = parsed === null || parsed < range.min || parsed > range.max;
  const error = invalid ? `请输入 ${range.min} 到 ${range.max} 之间的整数` : setting.error;
  return (
    <SettingRow
      label={label}
      description={description}
      error={error}
      note={invalid ? null : setting.note}
      {...ids}
    >
      <span className="setting-number">
        <input
          type="text"
          inputMode="numeric"
          value={text ?? String(setting.value)}
          onChange={(e) => {
            const raw = e.currentTarget.value;
            setText(raw);
            const n = parseWhole(raw);
            if (n !== null && n >= range.min && n <= range.max) setting.change(n);
          }}
          onBlur={() => {
            if (invalid) return;
            setting.flush();
            setText(null);
          }}
          aria-labelledby={ids.labelId}
          aria-describedby={
            [description ? ids.descriptionId : "", error ? ids.errorId : ""].join(" ").trim() || undefined
          }
          aria-invalid={invalid}
        />
        <span className="setting-unit" aria-hidden="true">
          {unit}
        </span>
      </span>
    </SettingRow>
  );
}

interface FolderListProps extends FieldProps<string[]> {
  /** The title of the system's folder picker, e.g. "选择要排除的文件夹". */
  pickerTitle: string;
  /** How many entries the list keeps (Settings.limits().maxPaths). */
  maxPaths: number;
}

function sameList(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((p, i) => p === b[i]);
}

/** FolderListSetting is a list of folders with a remove button each and 添加…, which opens the system's folder picker. Each change is saved at once. */
export function FolderListSetting({
  settings,
  spec,
  label,
  description,
  pickerTitle,
  maxPaths,
}: FolderListProps) {
  const ids = useRowIds();
  const setting = useSetting(settings, {
    equal: sameList,
    adjusted: () => "有重复或不完整的路径，已从列表中去掉",
    ...spec,
  });
  const [problem, setProblem] = useState<string | null>(null);
  const list = setting.value;
  const choose = useMutation({
    mutationFn: () => Settings.chooseFolder(pickerTitle),
    onSuccess: (path) => {
      // Cancelled.
      if (!path) return;
      // The settings package compares paths without regard to case.
      if (list.some((p) => p.toLowerCase() === path.toLowerCase())) {
        setProblem("这个文件夹已经在列表中了");
        return;
      }
      setting.change([...list, path]);
    },
    onError: (err) => setProblem(errorMessage(err)),
  });
  const full = list.length >= maxPaths;
  return (
    <SettingRow
      label={label}
      description={description}
      error={problem ?? setting.error}
      note={full ? `最多 ${maxPaths} 项` : setting.note}
      stacked
      {...ids}
    >
      <ul className="folder-list" aria-labelledby={ids.labelId}>
        {list.length === 0 && <li className="folder-empty">还没有添加文件夹</li>}
        {list.map((p) => (
          <li key={p} className="folder-row">
            <span className="folder-path" title={p}>
              {p}
            </span>
            <button
              type="button"
              className="icon-button"
              aria-label={`移除 ${p}`}
              onClick={() => {
                setProblem(null);
                setting.change(list.filter((x) => x !== p));
              }}
            >
              <Glyph size={14} color="currentColor">
                <path d="M6 6l12 12M18 6L6 18" />
              </Glyph>
            </button>
          </li>
        ))}
      </ul>
      <button
        type="button"
        className="ghost-button folder-add"
        disabled={full || choose.isPending}
        onClick={() => {
          setProblem(null);
          choose.mutate();
        }}
      >
        {choose.isPending ? "正在选择…" : "添加…"}
      </button>
    </SettingRow>
  );
}
