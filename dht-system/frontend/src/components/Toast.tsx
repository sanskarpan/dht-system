import { useEffect, useRef } from 'react';

import { type ToastType, useToastStore } from '@/components/toastStore';

interface ToastRecord {
  id: number;
  type: ToastType;
  message: string;
}

const DISMISS_DELAY = 4000; // ms

const typeStyles: Record<ToastType, string> = {
  success: 'bg-green-900/90 border-green-600 text-green-100',
  error:   'bg-red-900/90 border-red-600 text-red-100',
  info:    'bg-blue-900/90 border-blue-600 text-blue-100',
};

const iconMap: Record<ToastType, string> = {
  success: '✓',
  error:   '✕',
  info:    'i',
};

const iconBg: Record<ToastType, string> = {
  success: 'bg-green-600',
  error:   'bg-red-600',
  info:    'bg-blue-600',
};

function ToastItem({ toast: item }: { toast: ToastRecord }) {
  const remove = useToastStore((s) => s.remove);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    timerRef.current = setTimeout(() => remove(item.id), DISMISS_DELAY);
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, [item.id, remove]);

  return (
    <div
      className={`flex items-start gap-3 min-w-[260px] max-w-[360px] rounded-lg border px-4 py-3 shadow-xl backdrop-blur-sm animate-slide-in ${typeStyles[item.type]}`}
      role="alert"
    >
      <span
        className={`mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-[10px] font-bold text-white ${iconBg[item.type]}`}
      >
        {iconMap[item.type]}
      </span>
      <span className="text-sm leading-snug flex-1">{item.message}</span>
      <button
        onClick={() => remove(item.id)}
        className="ml-1 shrink-0 text-current opacity-50 hover:opacity-100 transition-opacity text-base leading-none"
        aria-label="Dismiss"
      >
        ×
      </button>
    </div>
  );
}

export default function ToastContainer() {
  const toasts = useToastStore((s) => s.toasts);

  return (
    <div
      className="fixed bottom-5 right-5 z-50 flex flex-col gap-2 pointer-events-none"
      aria-live="polite"
    >
      {toasts.map((t) => (
        <div key={t.id} className="pointer-events-auto">
          <ToastItem toast={t} />
        </div>
      ))}
    </div>
  );
}
