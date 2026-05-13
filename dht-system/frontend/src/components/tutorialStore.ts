import { create } from 'zustand';
import { persist } from 'zustand/middleware';

interface TutorialStore {
  active: boolean;
  step: number;
  start: () => void;
  next: () => void;
  skip: () => void;
}

interface Step {
  title: string;
  body: string;
}

export const STEPS: Step[] = [
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
      partialize: (state) => ({ active: state.active }),
    },
  ),
);

