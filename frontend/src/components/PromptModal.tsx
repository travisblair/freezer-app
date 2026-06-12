import { createSignal } from "solid-js";
import BaseModal from "./BaseModal";

interface Props {
  title: string;
  initialValue?: string;
  placeholder?: string;
  saveLabel?: string;
  onSave: (value: string) => void;
  onCancel: () => void;
}

/** Reusable modal for single text input forms (rename, create, etc.) */
export default function PromptModal(props: Props) {
  const [value, setValue] = createSignal(props.initialValue || "");

  function handleSubmit(e: Event) {
    e.preventDefault();
    const v = value().trim();
    if (!v) return;
    props.onSave(v);
  }

  const footer = (
    <>
      <button type="button" class="secondary" onClick={props.onCancel}>Cancel</button>
      <button type="submit" form="prompt-form">{props.saveLabel || "Save"}</button>
    </>
  );

  return (
    <BaseModal title={props.title} onClose={props.onCancel} footer={footer}>
      <form id="prompt-form" onSubmit={handleSubmit}>
        <input
          type="text"
          value={value()}
          onInput={e => setValue(e.target.value)}
          placeholder={props.placeholder}
          maxlength="100"
          autofocus
        />
      </form>
    </BaseModal>
  );
}
