import { onMount, onCleanup, type JSX } from "solid-js";

interface Props {
  title: string;
  onClose: () => void;
  children?: JSX.Element;
  footer?: JSX.Element;
  dialogRef?: (el: HTMLDialogElement) => void;
}

/**
 * Reusable modal wrapper.
 * Handles dialog lifecycle (show/close on mount/cleanup).
 * Only fires onClose once per dismissal action.
 */
export default function BaseModal(props: Props) {
  let dialogRef: HTMLDialogElement | undefined;

  onMount(() => {
    if (dialogRef) {
      dialogRef.showModal();
      props.dialogRef?.(dialogRef);
    }
  });

  function close() {
    if (dialogRef) dialogRef.close();
  }

  onCleanup(() => {
    // Cleanup fires props.onClose through the native onClose handler
    if (dialogRef && dialogRef.open) {
      dialogRef.close();
    }
  });

  return (
    <dialog ref={dialogRef} onClose={props.onClose}>
      <article>
        <header>
          <button
            type="button"
            aria-label="Close"
            class="pico-prev"
            onClick={close}
          />
          <p>
            <strong>{props.title}</strong>
          </p>
        </header>

        {props.children}

        {props.footer && <footer>{props.footer}</footer>}
      </article>
    </dialog>
  );
}