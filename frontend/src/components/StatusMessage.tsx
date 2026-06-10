import { Show } from "solid-js";
import type { StatusFeedback } from "../types";

interface Props {
  message: StatusFeedback | null;
}

export default function StatusMessage(props: Props) {
  return (
    <Show when={props.message}>
      <article class={`status-message ${props.message!.type}`}>
        {props.message!.text}
      </article>
    </Show>
  );
}