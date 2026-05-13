import { useTutorialStore, STEPS } from '@/components/tutorialStore';

export default function TutorialOverlay() {
  const active = useTutorialStore((s) => s.active);
  const step = useTutorialStore((s) => s.step);
  const next = useTutorialStore((s) => s.next);
  const skip = useTutorialStore((s) => s.skip);

  if (!active) return null;

  const current = STEPS[step];
  const isLast = step === STEPS.length - 1;
  const stepLabel = `${step + 1} / ${STEPS.length}`;

  return (
    /* Backdrop */
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
      aria-modal="true"
      role="dialog"
      aria-labelledby="tutorial-title"
    >
      {/* Card */}
      <div
        className="
          bg-slate-900 border border-slate-700 rounded-xl p-6 shadow-2xl
          max-w-sm w-full mx-4
        "
      >
        {/* Step counter */}
        <p className="text-xs text-slate-500 font-mono mb-3 tracking-wide uppercase">
          Step {stepLabel}
        </p>

        {/* Progress bar */}
        <div className="h-0.5 bg-slate-800 rounded-full mb-5 overflow-hidden">
          <div
            className="h-full bg-indigo-500 rounded-full transition-all duration-300"
            style={{ width: `${((step + 1) / STEPS.length) * 100}%` }}
          />
        </div>

        {/* Title */}
        <h2
          id="tutorial-title"
          className="text-slate-100 font-semibold text-base mb-2"
        >
          {current.title}
        </h2>

        {/* Description */}
        <p className="text-slate-400 text-sm leading-relaxed mb-6">
          {current.body}
        </p>

        {/* Step dot indicators */}
        <div className="flex justify-center gap-1.5 mb-6">
          {STEPS.map((_, i) => (
            <span
              key={i}
              className={`block h-1.5 rounded-full transition-all duration-200 ${
                i === step
                  ? 'w-4 bg-indigo-400'
                  : i < step
                  ? 'w-1.5 bg-slate-500'
                  : 'w-1.5 bg-slate-700'
              }`}
            />
          ))}
        </div>

        {/* Actions */}
        <div className="flex items-center justify-between gap-3">
          <button
            onClick={skip}
            className="text-xs text-slate-500 hover:text-slate-300 transition-colors py-1 px-2 rounded"
          >
            Skip tour
          </button>

          <button
            onClick={next}
            className="
              text-sm font-medium
              bg-indigo-600 hover:bg-indigo-500
              text-white
              rounded-lg px-4 py-1.5
              transition-colors
              focus-visible:outline focus-visible:outline-2 focus-visible:outline-indigo-400
            "
          >
            {isLast ? 'Finish' : 'Next'}
          </button>
        </div>
      </div>
    </div>
  );
}
