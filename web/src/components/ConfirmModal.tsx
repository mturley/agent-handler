interface ConfirmModalProps {
  isOpen: boolean;
  title: string;
  message: string;
  onConfirm: () => void;
  onCancel: () => void;
}

export function ConfirmModal({
  isOpen,
  title,
  message,
  onConfirm,
  onCancel,
}: ConfirmModalProps) {
  if (!isOpen) return null;

  return (
    <div
      className="fixed inset-0 bg-black/70 flex items-center justify-center z-[999] p-5"
      onClick={onCancel}
    >
      <div
        className="bg-bg-secondary border border-[#333] rounded-lg p-6 max-w-[500px] w-full shadow-[0_8px_16px_rgba(0,0,0,0.4)] max-[600px]:max-w-none"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-xl font-semibold mb-3 text-text-primary">{title}</h2>
        <p className="text-text-secondary mb-6 leading-relaxed">{message}</p>
        <div className="flex gap-3 justify-end max-[600px]:flex-col-reverse">
          <button
            className="px-4 py-2 rounded-md border-none text-[0.9rem] cursor-pointer transition-opacity duration-200 hover:opacity-80 bg-bg-tertiary text-text-primary max-[600px]:w-full"
            onClick={onCancel}
          >
            Cancel
          </button>
          <button
            className="px-4 py-2 rounded-md border-none text-[0.9rem] cursor-pointer transition-opacity duration-200 hover:opacity-80 bg-danger text-white max-[600px]:w-full"
            onClick={onConfirm}
          >
            Confirm
          </button>
        </div>
      </div>
    </div>
  );
}
