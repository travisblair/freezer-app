import { Show } from "solid-js";

interface Props {
  count: number;
  onClick: () => void;
}

export default function NotificationsBell(props: Props) {
  return (
    <button type="button" class="notif-bell" onClick={props.onClick} title="Notifications">
      🔔
      <Show when={props.count > 0}>
        <span class="notif-badge">
          {props.count > 99 ? "99+" : props.count}
        </span>
      </Show>
    </button>
  );
}
