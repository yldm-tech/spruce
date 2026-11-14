import "./switch.css";

interface SwitchProps {
  checked: boolean;
  onChange: (checked: boolean) => void;
  /** The id of the visible label; or label for a switch without one. */
  labelledBy?: string;
  label?: string;
  describedBy?: string;
  disabled?: boolean;
}

/** Switch is the 维护 cards' on/off switch for any page: a real checkbox with role="switch" lies invisible over the drawn track, so clicks, keyboard and screen readers all reach it. */
export function Switch({ checked, onChange, labelledBy, label, describedBy, disabled }: SwitchProps) {
  return (
    <span className="toggle-switch">
      <input
        type="checkbox"
        role="switch"
        checked={checked}
        aria-checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.currentTarget.checked)}
        aria-labelledby={labelledBy}
        aria-label={labelledBy ? undefined : label}
        aria-describedby={describedBy}
      />
      <span aria-hidden="true" className="toggle-switch-track">
        <span className="toggle-switch-thumb" />
      </span>
    </span>
  );
}
