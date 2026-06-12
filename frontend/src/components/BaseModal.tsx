import type { JSX } from "solid-js";

interface Props {
  title: string;
  onClose: () => void;
  children?: JSX.Element;
  footer?: JSX.Element;
  dialogRef?: (el: HTMLDialogElement) => void;
}

export default function BaseModal(props: Props) {
  return (
    <div class="modal-overlay" onClick={props.onClose}>
      <div class="modal-dialog" onClick={e => e.stopPropagation()}>
        <article>
          <header style="display:flex;align-items:center;justify-content:space-between">
            <strong>{props.title}</strong>
            <button
              type="button"
              aria-label="Close"
              class="pico-prev"
              onClick={props.onClose}
            />
          </header>

          {props.children}

          {props.footer && <footer style="display:flex;gap:8px;justify-content:flex-end">{props.footer}</footer>}
        </article>
      </div>
    </div>
  );
}
