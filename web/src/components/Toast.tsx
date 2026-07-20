import { useState, useCallback } from 'react';
import { ToastContext, type ToastType } from '../hooks/useToast';

interface Toast {
  id: number;
  message: string;
  type: ToastType;
}

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [nextId, setNextId] = useState(0);

  const showToast = useCallback((message: string, type: ToastType) => {
    const id = nextId;
    setNextId((prev) => prev + 1);
    setToasts((prev) => [...prev, { id, message, type }]);

    // Auto-dismiss after 3 seconds
    setTimeout(() => {
      setToasts((prev) => prev.filter((toast) => toast.id !== id));
    }, 3000);
  }, [nextId]);

  return (
    <ToastContext.Provider value={{ showToast }}>
      {children}
      <div className="fixed bottom-5 right-5 flex flex-col gap-2.5 z-[1000] pointer-events-none max-[600px]:left-5">
        {toasts.map((toast) => (
          <div
            key={toast.id}
            className={`bg-bg-secondary text-text-primary px-4 py-3 rounded-md border-l-4 shadow-[0_4px_6px_rgba(0,0,0,0.3)] min-w-[250px] max-w-[400px] animate-[toast-slide-in_0.3s_ease-out] pointer-events-auto max-[600px]:max-w-none
              ${toast.type === 'success' ? 'border-l-success' : 'border-l-danger'}`}
          >
            {toast.message}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}
