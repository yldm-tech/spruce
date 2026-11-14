import { useEffect, useRef, type ReactNode, type RefObject } from "react";

import { useBlockShell } from "../ShellBlock";
import { InOverlayHost } from "./OverlayHost";

import "./common.css";

interface GlassDialogProps {
  /** The id of the element naming the dialog (its title), or label for a dialog without a visible title. */
  labelledBy?: string;
  label?: string;
  /** The ids of the elements describing it, space-separated. */
  describedBy?: string;
  /** Called on Escape; a dialog without it cannot be dismissed that way. */
  onClose?: () => void;
  /** What takes focus when the dialog opens; by default its first button, link or field. */
  initialFocus?: RefObject<HTMLElement | null>;
  /** Extra class names for the card. */
  className?: string;
  children: ReactNode;
}

const FOCUSABLE =
  'button:not([disabled]), a[href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/** GlassDialog is the app's modal: the blurred scrim over the main area with a glass card, as the 维护 done card. While it is open the sidebar and the page are inert (ShellBlock), it is drawn in the OverlayHost outside the page, Tab cycles inside it, Escape calls onClose, and focus returns to where it was when it closes. */
export function GlassDialog({
  labelledBy,
  label,
  describedBy,
  onClose,
  initialFocus,
  className,
  children,
}: GlassDialogProps) {
  useBlockShell(true);
  const cardRef = useRef<HTMLDivElement>(null);
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  });

  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const card = cardRef.current;
    if (!card) return;
    const target = initialFocus?.current ?? card.querySelector<HTMLElement>(FOCUSABLE) ?? card;
    target.focus();

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && onCloseRef.current) {
        e.stopPropagation();
        onCloseRef.current();
        return;
      }
      if (e.key !== "Tab") return;
      const items = [...card.querySelectorAll<HTMLElement>(FOCUSABLE)];
      const first = items[0];
      const last = items[items.length - 1];
      if (!first || !last) {
        e.preventDefault();
        return;
      }
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    };
    card.addEventListener("keydown", onKeyDown);
    return () => {
      card.removeEventListener("keydown", onKeyDown);
      // The page under the dialog becomes usable again only after this render, so focus moves back on the next task, and only if nothing else took it meanwhile.
      setTimeout(() => {
        const active = document.activeElement;
        if (previous?.isConnected && (active === null || active === document.body)) previous.focus();
      }, 0);
    };
    // initialFocus is a ref object, the same on every render, so this runs once, when the dialog opens.
  }, [initialFocus]);

  return (
    <InOverlayHost>
      <div className="overlay overlay--dialog">
        <div
          ref={cardRef}
          role="dialog"
          aria-modal="true"
          aria-labelledby={labelledBy}
          aria-label={labelledBy ? undefined : label}
          aria-describedby={describedBy}
          tabIndex={-1}
          className={className ? `glass-card ${className}` : "glass-card"}
        >
          {children}
        </div>
      </div>
    </InOverlayHost>
  );
}
