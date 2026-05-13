import { useState, useEffect, useRef } from 'react';
import { useQuery, useMutation } from '@tanstack/react-query';
import { useDHTStore } from '@/store/dhtStore';
import type { DHTStore } from '@/store/dhtStore';
import type { ScenarioStepPayload } from '@/types/dht';
import * as api from '@/api/client';

interface Scenario {
  name: string;
  description: string;
}

interface ScenarioStep {
  step: number;
  total: number;
  message: string;
  data: unknown;
  timestamp: string;
}

interface ResultSummary {
  scenarioName: string;
  totalNodes: number;
  totalKeys: number;
  stepCount: number;
  steps: ScenarioStep[];
  completedAt: string;
  // Extracted from step data
  convergenceTime?: number;
  observedHops?: number[];
}

function extractSummaryFromSteps(
  scenarioName: string,
  steps: ScenarioStep[],
  nodeCount: number,
  keyCount: number
): ResultSummary {
  const convergenceTimes: number[] = [];
  const hops: number[] = [];

  for (const s of steps) {
    const d = s.data as Record<string, unknown> | null | undefined;
    if (d) {
      if (typeof d.convergenceTimeMs === 'number') convergenceTimes.push(d.convergenceTimeMs);
      if (typeof d.convergenceMs === 'number') convergenceTimes.push(d.convergenceMs);
      if (typeof d.totalHops === 'number') hops.push(d.totalHops);
      if (typeof d.hops === 'number') hops.push(d.hops);
      if (typeof d.avgHops === 'number') hops.push(Number(d.avgHops));
    }
  }

  return {
    scenarioName,
    totalNodes: nodeCount,
    totalKeys: keyCount,
    stepCount: steps.length,
    steps,
    completedAt: new Date().toLocaleTimeString(),
    convergenceTime: convergenceTimes.length > 0 ? convergenceTimes[convergenceTimes.length - 1] : undefined,
    observedHops: hops.length > 0 ? hops : undefined,
  };
}

export default function ScenarioRunner() {
  const nodes = useDHTStore((s: DHTStore) => s.nodes);
  const keys = useDHTStore((s: DHTStore) => s.keys);
  const events = useDHTStore((s: DHTStore) => s.events);

  const [running, setRunning] = useState<string | null>(null);
  const [resetting, setResetting] = useState(false);
  const [output, setOutput] = useState<string[]>([]);

  // Live steps for the actively running scenario
  const [liveSteps, setLiveSteps] = useState<ScenarioStep[]>([]);
  const liveStepsRef = useRef<ScenarioStep[]>([]);

  // Result summary shown after completion
  const [resultSummary, setResultSummary] = useState<ResultSummary | null>(null);
  const [showResults, setShowResults] = useState(false);

  // Completion timer ref: detect completion either by explicit final step or by inactivity
  const completionTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const { data: scenarios } = useQuery({
    queryKey: ['scenarios'],
    queryFn: api.listScenarios,
  });

  // Watch event store for scenario_step events matching the running scenario
  useEffect(() => {
    if (!running) return;

    const stepEvents = events
      .filter(e => e.type === 'scenario_step')
      .map(e => {
        const p = e.payload as ScenarioStepPayload;
        return {
          step: p.step,
          total: p.total,
          message: p.message,
          data: p.data,
          timestamp: e.timestamp,
          scenario: p.scenario,
        };
      })
      .filter(s => s.scenario === running)
      // Sort ascending by step number
      .sort((a, b) => a.step - b.step);

    const newSteps: ScenarioStep[] = stepEvents.map(({ step, total, message, data, timestamp }) => ({
      step,
      total,
      message,
      data,
      timestamp,
    }));

    liveStepsRef.current = newSteps;
    setLiveSteps(newSteps);

    if (newSteps.length > 0) {
      const last = newSteps[newSteps.length - 1];
      const finalizeAfterMs = last.total > 0 && last.step >= last.total ? 800 : 3000;
      if (completionTimerRef.current) clearTimeout(completionTimerRef.current);
      completionTimerRef.current = setTimeout(() => {
        const summary = extractSummaryFromSteps(
          running,
          liveStepsRef.current,
          nodes.size,
          keys.size
        );
        setResultSummary(summary);
        setRunning(null);
      }, finalizeAfterMs);
    }
  }, [events, running, nodes.size, keys.size]);

  // Cleanup timer on unmount
  useEffect(() => {
    return () => {
      if (completionTimerRef.current) clearTimeout(completionTimerRef.current);
    };
  }, []);

  const runMut = useMutation({
    mutationFn: (name: string) => api.runScenario(name),
    onMutate: (name) => {
      setRunning(name);
      setLiveSteps([]);
      liveStepsRef.current = [];
      setResultSummary(null);
      setShowResults(false);
      setOutput([`Starting scenario: ${name}...`]);
    },
    onSuccess: (_data, name) => {
      setOutput(prev => [
        ...prev,
        `Scenario "${name}" triggered. Waiting for step events...`,
      ]);
    },
    onError: (err) => {
      setOutput(prev => [...prev, `Error: ${err instanceof Error ? err.message : 'Unknown error'}`]);
      setRunning(null);
    },
  });

  const scenarioList: Scenario[] = scenarios?.scenarios ?? [];

  const handleReset = async () => {
    if (running) return;
    setResetting(true);
    try {
      await api.resetNetwork();
      setOutput(['Network reset successfully.']);
      setLiveSteps([]);
      liveStepsRef.current = [];
      setResultSummary(null);
      setShowResults(false);
    } catch (err) {
      setOutput([`Reset failed: ${err instanceof Error ? err.message : 'Unknown error'}`]);
    } finally {
      setResetting(false);
    }
  };

  const handleViewResults = () => {
    if (!running) {
      setShowResults(true);
      return;
    }
    // Manually generate summary from current live steps
    const summary = extractSummaryFromSteps(
      running,
      liveStepsRef.current,
      nodes.size,
      keys.size
    );
    setResultSummary(summary);
    setShowResults(true);
  };

  const handleClose = () => {
    setShowResults(false);
    setResultSummary(null);
    setLiveSteps([]);
    liveStepsRef.current = [];
    setOutput([]);
  };

  return (
    <div className="p-4 sm:p-6 max-w-3xl mx-auto space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <h2 className="text-slate-200 font-semibold">Scenario Runner</h2>
        <button
          onClick={handleReset}
          disabled={resetting || running !== null}
          className="px-3 py-1.5 bg-red-900/60 hover:bg-red-800/70 text-red-300 hover:text-red-200 text-xs rounded border border-red-800 hover:border-red-700 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
          title="Reset the network to a clean state"
        >
          {resetting ? 'Resetting...' : 'Reset Network'}
        </button>
      </div>

      {/* Scenario list */}
      <div className="grid gap-3">
        {scenarioList.map((s) => (
          <div
            key={s.name}
            className="bg-slate-900 rounded border border-slate-800 p-4 flex items-center justify-between"
          >
            <div>
              <p className="text-slate-200 text-sm font-medium capitalize">{s.name}</p>
              <p className="text-slate-500 text-xs mt-0.5">{s.description}</p>
            </div>
            <div className="flex gap-2">
              {(running === s.name || resultSummary?.scenarioName === s.name) && (
                <button
                  onClick={handleViewResults}
                  className="px-3 py-1.5 bg-slate-700 hover:bg-slate-600 text-slate-300 text-xs rounded"
                >
                  {running === s.name ? 'View Live' : 'Results'}
                </button>
              )}
              <button
                onClick={() => runMut.mutate(s.name)}
                disabled={runMut.isPending || running !== null}
                className="px-3 py-1.5 bg-purple-700 hover:bg-purple-600 text-white text-xs rounded disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {running === s.name ? 'Running...' : 'Run'}
              </button>
            </div>
          </div>
        ))}
        {scenarioList.length === 0 && (
          <p className="text-slate-500 text-sm">Loading scenarios...</p>
        )}
      </div>

      {/* Output log */}
      {output.length > 0 && (
        <div className="bg-slate-900 rounded border border-slate-800 p-4">
          <p className="text-xs text-slate-500 mb-2 uppercase tracking-wider">Log</p>
          {output.map((line, i) => (
            <p key={i} className="text-xs font-mono text-slate-300">
              {line}
            </p>
          ))}
        </div>
      )}

      {/* Live step-by-step display */}
      {running && liveSteps.length > 0 && (
        <div className="bg-slate-900 rounded border border-slate-800 p-4">
          <div className="flex items-center justify-between mb-3">
            <div>
              <p className="text-xs text-slate-500 uppercase tracking-wider">
                Live Steps &mdash;{' '}
                <span className="text-purple-400 normal-case font-medium">{running}</span>
              </p>
            </div>
            <div className="flex items-center gap-2">
              {/* Progress fraction */}
              {liveSteps.length > 0 && (
                <span className="text-xs text-slate-400 font-mono">
                  {liveSteps[liveSteps.length - 1].total > 0
                    ? `${liveSteps[liveSteps.length - 1].step} / ${liveSteps[liveSteps.length - 1].total}`
                    : `step ${liveSteps[liveSteps.length - 1].step}`}
                </span>
              )}
              {/* Pulsing indicator */}
              <span className="inline-block w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
            </div>
          </div>

          {/* Progress bar */}
          {liveSteps.length > 0 && liveSteps[liveSteps.length - 1].total > 0 && (
            <div className="bg-slate-800 rounded-full h-1.5 mb-4 overflow-hidden">
              <div
                className="h-full bg-purple-500 rounded-full transition-all duration-300"
                style={{
                  width: `${Math.min(
                    100,
                    (liveSteps[liveSteps.length - 1].step /
                      liveSteps[liveSteps.length - 1].total) *
                      100
                  )}%`,
                }}
              />
            </div>
          )}

          {/* Steps list */}
          <ol className="space-y-2 max-h-64 overflow-y-auto pr-1">
            {liveSteps.map((s) => (
              <li key={s.step} className="flex gap-3 text-xs">
                {/* Step number badge */}
                <span className="flex-shrink-0 w-6 h-6 rounded-full bg-slate-800 border border-slate-700 text-slate-400 flex items-center justify-center font-mono text-[10px]">
                  {s.step}
                </span>
                <div className="flex-1 min-w-0">
                  <p className="text-slate-200">{s.message}</p>
                  {!!s.data && typeof s.data === 'object' && Object.keys(s.data as object).length > 0 && (
                    <p className="font-mono text-slate-500 text-[10px] mt-0.5 truncate">
                      {JSON.stringify(s.data as Record<string, unknown>)}
                    </p>
                  )}
                </div>
                <span className="flex-shrink-0 text-slate-600 text-[10px] tabular-nums">
                  {new Date(s.timestamp).toLocaleTimeString()}
                </span>
              </li>
            ))}
          </ol>

          <div className="mt-3 pt-3 border-t border-slate-800 flex gap-2">
            <button
              onClick={handleViewResults}
              className="px-3 py-1.5 bg-slate-700 hover:bg-slate-600 text-slate-300 text-xs rounded"
            >
              View Summary
            </button>
          </div>
        </div>
      )}

      {/* Result Summary panel */}
      {showResults && resultSummary && (
        <div className="bg-slate-900 rounded border border-slate-700 p-4">
          <div className="flex items-center justify-between mb-4">
            <div>
              <p className="text-sm text-slate-200 font-medium capitalize">
                {resultSummary.scenarioName}
              </p>
              <p className="text-xs text-slate-500">
                Completed at {resultSummary.completedAt}
              </p>
            </div>
            <button
              onClick={handleClose}
              className="text-slate-500 hover:text-slate-300 text-xs px-2 py-1 rounded border border-slate-700 hover:border-slate-600"
            >
              Close
            </button>
          </div>

          {/* Summary stats grid */}
          <div className="grid grid-cols-1 gap-3 mb-4 sm:grid-cols-3">
            <div className="bg-slate-800 rounded p-3 text-center">
              <p className="text-xl font-bold text-slate-200">{resultSummary.totalNodes}</p>
              <p className="text-xs text-slate-500 mt-0.5">Nodes at End</p>
            </div>
            <div className="bg-slate-800 rounded p-3 text-center">
              <p className="text-xl font-bold text-slate-200">{resultSummary.totalKeys}</p>
              <p className="text-xs text-slate-500 mt-0.5">Total Keys</p>
            </div>
            <div className="bg-slate-800 rounded p-3 text-center">
              <p className="text-xl font-bold text-slate-200">{resultSummary.stepCount}</p>
              <p className="text-xs text-slate-500 mt-0.5">Steps</p>
            </div>
          </div>

          {/* Optional: convergence time */}
          {resultSummary.convergenceTime !== undefined && (
            <div className="bg-slate-800 rounded p-3 mb-3 flex items-center justify-between">
              <span className="text-xs text-slate-400">Convergence Time</span>
              <span className="text-sm font-semibold text-emerald-400">
                {resultSummary.convergenceTime} ms
              </span>
            </div>
          )}

          {/* Optional: observed hop counts */}
          {resultSummary.observedHops && resultSummary.observedHops.length > 0 && (
            <div className="bg-slate-800 rounded p-3 mb-3">
              <p className="text-xs text-slate-400 mb-1">Observed Hop Counts</p>
              <p className="text-xs font-mono text-slate-300">
                {resultSummary.observedHops.join(', ')}
              </p>
              <p className="text-xs text-slate-500 mt-1">
                avg:{' '}
                {(
                  resultSummary.observedHops.reduce((a, b) => a + b, 0) /
                  resultSummary.observedHops.length
                ).toFixed(2)}{' '}
                hops
              </p>
            </div>
          )}

          {/* Collapsible full step list */}
          <details className="group">
            <summary className="text-xs text-slate-500 cursor-pointer select-none hover:text-slate-300 list-none flex items-center gap-1">
              <span className="group-open:rotate-90 transition-transform inline-block">&#9654;</span>
              All steps ({resultSummary.stepCount})
            </summary>
            <ol className="mt-2 space-y-1.5 max-h-48 overflow-y-auto">
              {resultSummary.steps.map((s) => (
                <li key={s.step} className="flex gap-2 text-xs">
                  <span className="flex-shrink-0 text-slate-600 font-mono w-5 text-right">
                    {s.step}.
                  </span>
                  <span className="text-slate-300">{s.message}</span>
                </li>
              ))}
            </ol>
          </details>
        </div>
      )}
    </div>
  );
}
