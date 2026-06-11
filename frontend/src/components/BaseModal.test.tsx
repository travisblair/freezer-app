import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import BaseModal from "./BaseModal";

// jsdom doesn't fully support <dialog>.showModal — polyfill it.
HTMLDialogElement.prototype.showModal = function () {
  this.setAttribute("open", "");
};
HTMLDialogElement.prototype.close = function () {
  this.removeAttribute("open");
};

describe("BaseModal", () => {
  it("renders title and children", () => {
    const onClose = vi.fn();
    render(() => (
      <BaseModal title="Test Modal" onClose={onClose}>
        <p>Modal content</p>
      </BaseModal>
    ));

    expect(screen.getByText("Test Modal")).toBeTruthy();
    expect(screen.getByText("Modal content")).toBeTruthy();
  });

  it("renders footer when provided", () => {
    render(() => (
      <BaseModal
        title="With Footer"
        onClose={vi.fn()}
        footer={<button>Save</button>}
      >
        <p>Body</p>
      </BaseModal>
    ));

    expect(screen.getByText("Save")).toBeTruthy();
  });

  it("calls dialogRef callback with the dialog element", () => {
    const dialogRef = vi.fn();
    render(() => (
      <BaseModal title="Ref Test" onClose={vi.fn()} dialogRef={dialogRef}>
        <p>Body</p>
      </BaseModal>
    ));

    expect(dialogRef).toHaveBeenCalledOnce();
    expect(dialogRef.mock.calls[0][0]).toBeInstanceOf(HTMLDialogElement);
  });
});
