import { useState } from 'react';

import type { Config } from '@/types/dht';

function SliderRow({
  label,
  value,
  min,
  max,
  step,
  format,
  onChange,
}: {
  label: string;
  value: number;
  min: number;
  max: number;
  step: number;
  format: (v: number) => string;
  onChange: (v: number) => void;
}) {
  return (
    <div className="mb-3">
      <div className="flex justify-between items-center mb-1">
        <span className="text-xs text-slate-400">{label}</span>
        <span className="text-xs font-mono bg-slate-800 text-slate-200 px-1.5 py-0.5 rounded">
          {format(value)}
        </span>
      </div>
      <input
        type="range"
        min={min}
        max={max}
        step={step}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        className="w-full h-1.5 rounded-full appearance-none cursor-pointer accent-blue-500 bg-slate-700"
      />
    </div>
  );
}

interface NetworkConfigPanelProps {
  config: Config;
  onChange: (cfg: Partial<Config>) => void;
}

export default function NetworkConfigPanel({ config, onChange }: NetworkConfigPanelProps) {
  const [writeQuorum, setWriteQuorum] = useState(config.writeQuorum);
  const [readQuorum, setReadQuorum] = useState(config.readQuorum);
  const [replicationN, setReplicationN] = useState(config.replicationN);
  const [stabilizeIntervalMs, setStabilizeIntervalMs] = useState(config.stabilizeIntervalMs);
  const [simDelayMs, setSimDelayMs] = useState(config.simDelayMs);
  const [simLossRate, setSimLossRate] = useState(Math.round(config.simLossRate * 100));

  return (
    <div>
      <p className="text-xs text-slate-500 uppercase tracking-wider mb-2">Network Config</p>
      <SliderRow
        label="Write Quorum (W)"
        value={writeQuorum}
        min={1}
        max={5}
        step={1}
        format={(v) => `${v}`}
        onChange={(v) => {
          setWriteQuorum(v);
          onChange({ writeQuorum: v });
        }}
      />
      <SliderRow
        label="Read Quorum (R)"
        value={readQuorum}
        min={1}
        max={5}
        step={1}
        format={(v) => `${v}`}
        onChange={(v) => {
          setReadQuorum(v);
          onChange({ readQuorum: v });
        }}
      />
      <SliderRow
        label="Replication N"
        value={replicationN}
        min={1}
        max={7}
        step={1}
        format={(v) => `${v}`}
        onChange={(v) => {
          setReplicationN(v);
          onChange({ replicationN: v });
        }}
      />
      <SliderRow
        label="Stabilize Interval"
        value={stabilizeIntervalMs}
        min={100}
        max={2000}
        step={50}
        format={(v) => `${v}ms`}
        onChange={(v) => {
          setStabilizeIntervalMs(v);
          onChange({ stabilizeIntervalMs: v });
        }}
      />
      <SliderRow
        label="Simulated Delay"
        value={simDelayMs}
        min={0}
        max={1000}
        step={10}
        format={(v) => `${v}ms`}
        onChange={(v) => {
          setSimDelayMs(v);
          onChange({ simDelayMs: v });
        }}
      />
      <SliderRow
        label="Loss Rate"
        value={simLossRate}
        min={0}
        max={50}
        step={1}
        format={(v) => `${v}%`}
        onChange={(v) => {
          setSimLossRate(v);
          onChange({ simLossRate: v / 100 });
        }}
      />
    </div>
  );
}

