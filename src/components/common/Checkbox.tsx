import { useEffect, useRef } from "react";

import "./common.css";

interface CheckboxProps {
  checked: boolean;
  /** Shows the dash of a partly checked group. */
  indeterminate?: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
  "aria-label"?: string;
  "aria-labelledby"?: string;
  "aria-describedby"?: string;
}

/** Checkbox is a real checkbox drawn in the app's style, with the tri-state of a category header. */
export function Checkbox({ checked, indeterminate = false, onChange, disabled, ...aria }: CheckboxProps) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (ref.current) ref.current.indeterminate = indeterminate;
  }, [indeterminate]);
  return (
    <span className="checkbox">
      <input
        ref={ref}
        type="checkbox"
        checked={checked}
        aria-checked={indeterminate ? "mixed" : checked}
        disabled={disabled}
        onChange={(e) => onChange(e.currentTarget.checked)}
        {...aria}
      />
      <span aria-hidden="true" className="checkbox-box">
        <svg
          width="12"
          height="12"
          viewBox="0 0 24 24"
          fill="none"
          strokeWidth="3.4"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          {indeterminate ? <path d="M6 12h12" /> : <path d="M5 12.5l4.5 4.5L19 7.5" />}
        </svg>
      </span>
    </span>
  );
}
