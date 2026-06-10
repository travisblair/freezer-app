import { useScanner } from "../hooks/useScanner";
import ScanPromptForm from "./ScanPromptForm";
import DuplicateOffer from "./DuplicateOffer";
import LinkBarcode from "./LinkBarcode";
import UnknownBarcodePrompt from "./UnknownBarcodePrompt";
import DecrementBlockedPrompt from "./DecrementBlockedPrompt";

export default function Scanner() {
  const sc = useScanner();

  return (
    <div>
      <button
        type="button"
        class={sc.expanded() ? "" : "outline"}
        onClick={sc.toggleExpanded}
      >
        {sc.expanded() ? "Stop Scanner" : "Start Scanner"}
      </button>

      {sc.expanded() && (
        <div class="mt-h">
          <div class="scanner-controls">
            <button
              type="button"
              class={sc.mode() === "increment" ? "" : "outline"}
              onClick={() => sc.setMode("increment")}
            >
              + Add
            </button>
            <button
              type="button"
              class={sc.mode() === "decrement" ? "" : "outline"}
              onClick={() => sc.setMode("decrement")}
            >
              − Remove
            </button>
            <label class="no-mb flex-row gap-1">
              Qty:
              <input
                type="number"
                min="1"
                max="9999"
                value={sc.quantity()}
                onInput={(e) =>
                  sc.setQuantity(parseInt((e.target as HTMLInputElement).value, 10) || 1)
                }
                class="no-mb"
              />
            </label>
            {!sc.scanning() && !sc.cameraError() && (
              <button type="button" onClick={sc.startCamera}>
                Start Camera
              </button>
            )}
            {sc.scanning() && (
              <button type="button" class="secondary" onClick={sc.stopCamera}>
                Stop Camera
              </button>
            )}
          </div>

          {sc.cameraError() && (
            <article class="article-warning mb-h">{sc.cameraError()}</article>
          )}

          <div id="reader" class="scanner-viewport" />

          {sc.feedback() && (
            <div
              class={`scanner-feedback ${
                sc.feedback()!.type === "success"
                  ? "scanner-feedback-success"
                  : "scanner-feedback-error"
              }`}
            >
              {sc.feedback()!.type === "success" ? "✔" : "✘"}
            </div>
          )}

          {/* Normal unknown barcode: Create new / Link / Ignore */}
          {sc.prompt() && !sc.prompt()!.mode && (
            <UnknownBarcodePrompt
              barcode={sc.prompt()!.barcode}
              onCreateNew={() => sc.showCreateForm()}
              onLinkToExisting={sc.handleOpenLinkModal}
              onCancel={sc.handleCancelPrompt}
            />
          )}

          {/* Decrement-blocked prompt: Switch to Add / Link / Cancel */}
          {sc.prompt() && sc.prompt()!.mode === "decrement-block" && (
            <DecrementBlockedPrompt
              barcode={sc.prompt()!.barcode}
              onCreateNew={() => sc.handleSwitchToAddAndCreate()}
              onLinkToExisting={sc.handleOpenLinkModal}
              onCancel={sc.handleCancelPrompt}
            />
          )}

          {/* Create-new form (name + qty) */}
          {sc.prompt() && sc.prompt()!.mode === "create" && (
            <ScanPromptForm
              barcode={sc.prompt()!.barcode}
              onSubmit={(name, qty) => sc.handleCreateNew(name, qty)}
              onCancel={sc.handleCancelPrompt}
            />
          )}

          {sc.duplicateOffer() && (
            <DuplicateOffer
              barcode={sc.duplicateOffer()!.barcode}
              existing={sc.duplicateOffer()!.existing}
              showModeToggle={true}
              onResolve={sc.handleDuplicateResolve}
              onDismiss={() => sc.setDuplicateOffer(null)}
            />
          )}

          {sc.linkBarcode() && (
            <LinkBarcode
              barcode={sc.linkBarcode()!}
              onConfirm={sc.handleConfirmLink}
              onCancel={sc.handleCancelLink}
            />
          )}
        </div>
      )}
    </div>
  );
}