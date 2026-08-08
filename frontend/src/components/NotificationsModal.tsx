import { For, Show } from "solid-js";
import type { AuditLog } from "../types";
import BaseModal from "./BaseModal";

interface Props {
  logs: AuditLog[];
  onClose: () => void;
}

/** Maps action codes to human-readable descriptions. */
function formatAction(log: AuditLog): string {
  try {
    const details = JSON.parse(log.details || "{}");
    switch (log.action) {
      case "scan":
        return `scanned ${details.mode === "decrement" ? "−" : "+"}${details.quantity} in ${details.shelf || "?"}`;
      case "create":
        return `added ${log.entity_name}`;
      case "update":
        return `renamed ${log.entity_name}`;
      case "delete":
        return `removed ${log.entity_name}`;
      case "bulk_delete":
        return `removed ${log.entity_name}`;
      case "hard_delete":
        return `permanently deleted ${log.entity_name}`;
      case "link_barcode":
        return `linked barcode to ${log.entity_name}`;
      case "move":
        return `moved ${log.entity_name} (${details.qty}) from shelf ${details.from_shelf} → ${details.to_shelf}`;
      case "set_count":
        return `set count to ${details.count}`;
      case "shelf_create":
        return `created shelf "${log.entity_name}"`;
      case "shelf_update":
        return `renamed shelf "${log.entity_name}"`;
      case "shelf_delete":
        return `deleted shelf "${log.entity_name}"`;
      case "list_create":
        return `created list "${log.entity_name}"`;
      case "list_update":
        return `renamed list "${log.entity_name}"`;
      case "list_delete":
        return `deleted list "${log.entity_name}"`;
      default:
        return log.action;
    }
  } catch {
    return log.action;
  }
}

function relativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const secs = Math.floor(diff / 1000);
  if (secs < 60) return "just now";
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  const days = Math.floor(hrs / 24);
  return `${days}d ago`;
}

export default function NotificationsModal(props: Props) {
  return (
    <BaseModal title="Recent Activity" onClose={props.onClose}>
      <div class="notif-list">
        <For each={props.logs}>
          {(log) => (
            <div class="notif-entry">
              <div class="notif-entry-header">
                <span class="notif-user">{log.user_name || "Unknown"}</span>
                <span class="notif-time">{relativeTime(log.created_at)}</span>
              </div>
              <div class="notif-detail">{formatAction(log)}</div>
            </div>
          )}
        </For>
        <Show when={props.logs.length === 0}>
          <p class="notif-empty">No activity yet</p>
        </Show>
      </div>
    </BaseModal>
  );
}
