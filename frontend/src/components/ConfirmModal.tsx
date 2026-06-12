import BaseModal from "./BaseModal";

interface Props {
  message: string;
  variant?: "danger";
  onConfirm: () => void;
  onCancel: () => void;
}

export default function ConfirmModal(props: Props) {
  return (
    <BaseModal
      title="Confirm"
      onClose={props.onCancel}
      footer={
        <>
          <button type="button" class="secondary" onClick={props.onCancel}>
            Cancel
          </button>
          <button
            type="button"
            class={props.variant === "danger" ? "danger-confirm" : ""}
            onClick={props.onConfirm}
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
