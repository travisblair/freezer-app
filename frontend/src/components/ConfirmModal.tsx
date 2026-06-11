import BaseModal from "./BaseModal";

interface Props {
  message: string;
  variant?: "danger";
  onConfirm: () => void;
  onCancel: () => void;
}

export default function ConfirmModal(props: Props) {
  let dialogEl: HTMLDialogElement | undefined;

  function handleCancel() {
    if (dialogEl) dialogEl.close();
    props.onCancel();
  }

  function handleConfirm() {
    if (dialogEl) dialogEl.close();
    props.onConfirm();
  }

  return (
    <BaseModal
      title="Confirm"
      onClose={props.onCancel}
      dialogRef={(el) => { dialogEl = el; }}
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