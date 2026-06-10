import { onMount } from "solid-js";
import BaseModal from "./BaseModal";

interface Props {
  message: string;
  variant?: "danger";
  onConfirm: () => void;
  onCancel: () => void;
}

export default function ConfirmModal(props: Props) {
  let dialogRef: HTMLDialogElement | undefined;

  onMount(() => {
    // Grab the rendered <dialog> from BaseModal (it's the first child)
    const article = document.querySelector("dialog[open]");
    if (article instanceof HTMLDialogElement) dialogRef = article;
  });

  function handleCancel() {
    if (dialogRef) dialogRef.close();
    props.onCancel();
  }

  function handleConfirm() {
    if (dialogRef) dialogRef.close();
    props.onConfirm();
  }

  return (
    <BaseModal
      title="Confirm"
      onClose={props.onCancel}
      footer={
        <>
          <button type="button" class="secondary" onClick={handleCancel}>
            Cancel
          </button>
          <button
            type="button"
            class={props.variant === "danger" ? "danger-confirm" : ""}
            onClick={handleConfirm}
          >
            {props.variant === "danger" ? "Delete Forever" : "Confirm"}
          </button>
        </>
      }
    >
      <p>{props.message}</p>
    </BaseModal>
  );
}