import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { useEffect, useState } from 'react';

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

interface TutorialStore {
  active: boolean;
  step: number;
  start: () => void;
  next: () => void;
  skip: () => void;
}

export const useTutorialStore = create<TutorialStore>()(
  persist(
    (set, get) => ({
      active: true,
      step: 0,

      start: () => set({ active: true, step: 0 }),

      next: () => {
        const { step, skip } = get();
        if (step >= STEPS.length - 1) {
          skip();
        } else {
          set({ step: step + 1 });
        }
      },

      skip: () => set({ active: false }),
    }),
    {
      name: 'dht-tutorial-done',
      // Only persist whether the tutorial has been dismissed.
      // On first load the key is absent, so `active` stays true (default).
      // After skip() persists active=false, subsequent loads start inactive.
      partialize: (state) => ({ active: state.active }),
    },
  ),
);

// ---------------------------------------------------------------------------
// Step definitions
// ---------------------------------------------------------------------------

interface Step {
  title: string;
  body: string;
}

const STEPS: Step[] = [
  {
    title: 'Welcome to DHT Visualizer',
    body:
      'This tool lets you explore how a Distributed Hash Table (DHT) works in real time. ' +
      'Nodes form a logical ring and keys are assigned to them automatically. ' +
      'Follow this short tour to get up to speed quickly.',
  },
  {
    title: 'The Ring View',
    body:
      'The main canvas draws every live node as a circle arranged on a ring. ' +
      'Small dots on the ring represent keys stored in the network. ' +
      'Click any node circle to open the Node Inspector and see its routing table.',
  },
  {
    title: 'Add Nodes',
    body:
      'Click the [+ Add Node] button in the sidebar, or press N anywhere in the app, to join a new node to the ring. ' +
      'Watch how keys are redistributed as the ring re-stabilizes. ' +
      'You can add as many nodes as you like to see the ring grow.',
  },
  {
    title: 'Insert Keys',
    body:
      'Type a key name and an optional value into the insert form in the sidebar, then hit Enter to store data on the ring. ' +
      'Press K from anywhere to jump back to the ring view and focus that form instantly. ' +
      'The key dot will appear on the ring at the responsible node.',
  },
  {
    title: 'Trace Lookups',
    body:
      'Open the Lookup tab (top nav) or press L to jump straight there. ' +
      'Enter any key to watch the DHT route the request hop by hop until the responsible node is found. ' +
      'Each hop is highlighted on the ring so you can see the algorithm in action.',
  },
  {
    title: 'Explore More',
    body:
      'The Scenarios tab runs pre-built automated experiments such as node churn and mass inserts. ' +
      'The Consistency tab shows replication health and quorum status across the ring. ' +
      'The Metrics tab surfaces live charts for load distribution, latency, and more.',
  },
];

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export default function TutorialOverlay() {
  const active = useTutorialStore((s) => s.active);
  const step = useTutorialStore((s) => s.step);
  const next = useTutorialStore((s) => s.next);
  const skip = useTutorialStore((s) => s.skip);

  // Drive a CSS fade whenever the step changes.
  const [visible, setVisible] = useState(true);

  useEffect(() => {
    setVisible(false);
    const t = setTimeout(() => setVisible(true), 80);
    return () => clearTimeout(t);
  }, [step]);

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
        className={`
          bg-slate-900 border border-slate-700 rounded-xl p-6 shadow-2xl
          max-w-sm w-full mx-4
          transition-opacity duration-200
          ${visible ? 'opacity-100' : 'opacity-0'}
        `}
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
