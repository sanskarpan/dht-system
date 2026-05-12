import { useEffect, useRef, useMemo, useState } from 'react';
import * as d3 from 'd3';
import { useDHTStore } from '@/store/dhtStore';
import type { DHTStore } from '@/store/dhtStore';
import type { NodeSnapshot, KeySnapshot } from '@/types/dht';
import { useNavigate } from 'react-router-dom';

// Convert a hex ID string to an angle in radians [0, 2π)
// Positions start from top (−π/2) going clockwise
function hexToAngle(hexId: string): number {
  // Parse first 13 hex chars as BigInt (52 bits, enough for angle precision)
  const partial = hexId.slice(0, 13);
  const value = BigInt('0x' + partial);
  const max = BigInt('0x' + 'f'.repeat(13));
  const fraction = Number(value) / Number(max);
  return fraction * 2 * Math.PI - Math.PI / 2;
}

function angleToXY(angle: number, radius: number, cx: number, cy: number): [number, number] {
  return [cx + radius * Math.cos(angle), cy + radius * Math.sin(angle)];
}

// Color based on node status
function nodeColor(node: NodeSnapshot): string {
  switch (node.status) {
    case 'healthy': return '#22c55e';
    case 'stabilizing': return '#eab308';
    case 'unreachable': return '#ef4444';
    case 'crashed': return '#6b7280';
    default: return '#3b82f6';
  }
}

// Heat fill: cool blue (few keys) → amber → hot red (many keys)
function nodeHeatFill(keyCount: number, maxKeys: number): string {
  if (maxKeys <= 0 || keyCount <= 0) return '#1e3a5f';
  const t = Math.min(1, keyCount / maxKeys);
  // 4-stop gradient: #1e3a5f → #0d7377 → #d97706 → #dc2626
  const stops: [number, number, number][] = [
    [0x1e, 0x3a, 0x5f],
    [0x0d, 0x73, 0x77],
    [0xd9, 0x77, 0x06],
    [0xdc, 0x26, 0x26],
  ];
  const segment = t * (stops.length - 1);
  const i = Math.min(stops.length - 2, Math.floor(segment));
  const f = segment - i;
  const r = Math.round(stops[i][0] + f * (stops[i + 1][0] - stops[i][0]));
  const g = Math.round(stops[i][1] + f * (stops[i + 1][1] - stops[i][1]));
  const b = Math.round(stops[i][2] + f * (stops[i + 1][2] - stops[i][2]));
  return `rgb(${r},${g},${b})`;
}

// Color based on replica count
function keyColor(replicas: number): string {
  if (replicas >= 3) return '#22c55e';
  if (replicas === 2) return '#eab308';
  return '#94a3b8';
}

interface RingCanvasProps {
  width: number;
  height: number;
  showFingers: boolean;
}

export default function RingCanvas({ width, height, showFingers }: RingCanvasProps) {
  const svgRef = useRef<SVGSVGElement>(null);
  const navigate = useNavigate();

  const nodeMap = useDHTStore((s: DHTStore) => s.nodes);
  const keyMap = useDHTStore((s: DHTStore) => s.keys);
  const selectedNode = useDHTStore((s: DHTStore) => s.selectedNode);
  const setSelectedNode = useDHTStore((s: DHTStore) => s.setSelectedNode);
  const activeLookup = useDHTStore((s: DHTStore) => s.activeLookup);

  // Local state: which key dot is highlighted (shows replica nodes)
  const [highlightedKeyId, setHighlightedKeyId] = useState<string | null>(null);

  const cx = width / 2;
  const cy = height / 2;
  const ringRadius = Math.min(width, height) * 0.38;
  const nodes = useMemo(() => Array.from(nodeMap.values()), [nodeMap]);
  const keys = useMemo(() => Array.from(keyMap.values()), [keyMap]);

  // Compute node positions
  const nodePositions = useMemo(() => {
    return new Map(
      nodes.map((n) => [n.id, angleToXY(hexToAngle(n.id), ringRadius, cx, cy)])
    );
  }, [nodes, ringRadius, cx, cy]);

  // Compute highlighted replica set from selected key
  const highlightedReplicas = useMemo(() => {
    if (!highlightedKeyId) return new Set<string>();
    const key = (keys as KeySnapshot[]).find((k) => k.id === highlightedKeyId);
    return new Set<string>(key?.replicas ?? []);
  }, [highlightedKeyId, keys]);

  useEffect(() => {
    if (!svgRef.current) return;
    const svgEl = svgRef.current;

    const svg = d3.select<SVGSVGElement, unknown>(svgEl);
    svg.selectAll('*').remove();

    const g = svg.append('g');

    // Zoom behavior — typed against the concrete SVGSVGElement (not | null)
    const zoom = d3.zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.3, 5])
      .on('zoom', (event) => {
        g.attr('transform', event.transform.toString());
      });
    svg.call(zoom);

    // Background
    svg.append('rect')
      .attr('width', width)
      .attr('height', height)
      .attr('fill', '#0a0a0f');

    // Ring circle
    g.append('circle')
      .attr('cx', cx)
      .attr('cy', cy)
      .attr('r', ringRadius)
      .attr('fill', 'none')
      .attr('stroke', '#1e293b')
      .attr('stroke-width', 2);

    // Finger rays (optional)
    if (showFingers) {
      nodes.forEach((node) => {
        const from = nodePositions.get(node.id);
        if (!from || !node.fingerTable) return;
        node.fingerTable.forEach((finger, i) => {
          if (!finger.nodeId || finger.nodeId === node.id) return;
          const to = nodePositions.get(finger.nodeId);
          if (!to) return;
          const opacity = Math.max(0.05, 0.5 - i * 0.003);
          g.append('line')
            .attr('x1', from[0]).attr('y1', from[1])
            .attr('x2', to[0]).attr('y2', to[1])
            .attr('stroke', '#3b82f6')
            .attr('stroke-width', 0.5)
            .attr('opacity', opacity);
        });
      });
    }

    // Successor arcs
    nodes.forEach((node) => {
      if (!node.successor) return;
      const from = nodePositions.get(node.id);
      const to = nodePositions.get(node.successor);
      if (!from || !to) return;

      const fromAngle = hexToAngle(node.id);
      const toAngle = hexToAngle(node.successor);

      // Draw arc along the ring
      let sweep = toAngle - fromAngle;
      if (sweep < 0) sweep += 2 * Math.PI;

      const arcGen = d3.arc<{ startAngle: number; endAngle: number }>()
        .innerRadius(ringRadius - 3)
        .outerRadius(ringRadius + 3)
        .padAngle(0.02)
        .cornerRadius(0);

      const arcPath = arcGen({
        startAngle: fromAngle + Math.PI / 2,
        endAngle: fromAngle + Math.PI / 2 + sweep,
      });

      if (arcPath) {
        g.append('path')
          .attr('d', arcPath)
          .attr('transform', `translate(${cx},${cy})`)
          .attr('fill', nodeColor(node))
          .attr('opacity', 0.3);
      }
    });

    // ── Lookup hop animation ─────────────────────────────────────────────────
    // Draw animated arcs for each hop in the active lookup trace.
    if (activeLookup && activeLookup.hops.length > 0) {
      // Draw hop lines with staggered delay so they appear sequentially
      activeLookup.hops.forEach((hop, i) => {
        const fromPos = nodePositions.get(hop.fromNode);
        const toPos = nodePositions.get(hop.toNode);
        if (!fromPos || !toPos) return;

        const [fx, fy] = fromPos;
        const [tx, ty] = toPos;

        // Draw the static line for this hop (fades after a while)
        const line = g.append('line')
          .attr('x1', fx).attr('y1', fy)
          .attr('x2', tx).attr('y2', ty)
          .attr('stroke', '#f59e0b')
          .attr('stroke-width', 1.5)
          .attr('opacity', 0)
          .attr('stroke-dasharray', '4 3');

        line.transition()
          .delay(i * 200)
          .duration(300)
          .attr('opacity', 0.7)
          .transition()
          .delay(1500)
          .duration(800)
          .attr('opacity', 0.15);

        // Animated pulse circle travelling from→to along the hop line
        const pulse = g.append('circle')
          .attr('cx', fx)
          .attr('cy', fy)
          .attr('r', 5)
          .attr('fill', '#fbbf24')
          .attr('opacity', 0);

        pulse.transition()
          .delay(i * 200)
          .duration(0)
          .attr('opacity', 0.9)
          .transition()
          .duration(350)
          .ease(d3.easeCubicOut)
          .attr('cx', tx)
          .attr('cy', ty)
          .transition()
          .duration(300)
          .attr('r', 10)
          .attr('opacity', 0);

        // Hop index badge at midpoint
        const mx = (fx + tx) / 2;
        const my = (fy + ty) / 2;
        const badge = g.append('text')
          .attr('x', mx)
          .attr('y', my - 6)
          .attr('text-anchor', 'middle')
          .attr('font-size', 8)
          .attr('font-family', 'monospace')
          .attr('fill', '#fbbf24')
          .attr('opacity', 0)
          .text(`#${i + 1}`);

        badge.transition()
          .delay(i * 200 + 150)
          .duration(200)
          .attr('opacity', 0.8)
          .transition()
          .delay(1200)
          .duration(600)
          .attr('opacity', 0);
      });

      // Glow ring on final target node
      const lastHop = activeLookup.hops[activeLookup.hops.length - 1];
      const targetPos = nodePositions.get(lastHop.toNode);
      if (targetPos) {
        const [tx, ty] = targetPos;
        const glow = g.append('circle')
          .attr('cx', tx)
          .attr('cy', ty)
          .attr('r', 14)
          .attr('fill', 'none')
          .attr('stroke', '#f59e0b')
          .attr('stroke-width', 3)
          .attr('opacity', 0);

        glow.transition()
          .delay(activeLookup.hops.length * 200 + 100)
          .duration(300)
          .attr('opacity', 0.9)
          .transition()
          .duration(600)
          .attr('r', 26)
          .attr('opacity', 0);
      }
    }
    // ── End lookup animation ─────────────────────────────────────────────────

    // Key dots on ring
    (keys as KeySnapshot[]).forEach((key) => {
      const angle = hexToAngle(key.id);
      const [kx, ky] = angleToXY(angle, ringRadius, cx, cy);
      const isHighlighted = key.id === highlightedKeyId;

      // Highlight ring behind selected key dot
      if (isHighlighted) {
        g.append('circle')
          .attr('cx', kx)
          .attr('cy', ky)
          .attr('r', 7)
          .attr('fill', 'none')
          .attr('stroke', '#f59e0b')
          .attr('stroke-width', 1.5)
          .attr('opacity', 0.8);
      }

      g.append('circle')
        .attr('cx', kx)
        .attr('cy', ky)
        .attr('r', isHighlighted ? 4 : 3)
        .attr('fill', isHighlighted ? '#fbbf24' : keyColor(key.replicaCount))
        .attr('cursor', 'pointer')
        .on('click', (event) => {
          event.stopPropagation();
          setHighlightedKeyId((prev) => (prev === key.id ? null : key.id));
        })
        .append('title')
        .text(
          `Key: ${key.id.slice(0, 8)}...\nReplicas: ${key.replicaCount}\nNodes: ${(key.replicas ?? []).map((r) => r.slice(0, 6)).join(', ')}`
        );
    });

    // ── Heat legend ───────────────────────────────────────────────────────────
    if (nodes.length > 0) {
      const lx = 14;
      const ly = height - 36;
      const lw = 72;
      const lh = 6;

      const defs = svg.append('defs');
      const grad = defs.append('linearGradient').attr('id', 'heat-grad-legend').attr('x1', '0%').attr('x2', '100%');
      grad.append('stop').attr('offset', '0%').attr('stop-color', '#1e3a5f');
      grad.append('stop').attr('offset', '33%').attr('stop-color', '#0d7377');
      grad.append('stop').attr('offset', '67%').attr('stop-color', '#d97706');
      grad.append('stop').attr('offset', '100%').attr('stop-color', '#dc2626');

      svg.append('text').attr('x', lx).attr('y', ly - 4)
        .attr('font-size', 7).attr('fill', '#475569').attr('font-family', 'monospace')
        .text('Key density');

      svg.append('rect')
        .attr('x', lx).attr('y', ly).attr('width', lw).attr('height', lh)
        .attr('rx', 2).attr('fill', 'url(#heat-grad-legend)').attr('opacity', 0.75);

      svg.append('text').attr('x', lx).attr('y', ly + lh + 8)
        .attr('font-size', 6).attr('fill', '#334155').attr('font-family', 'monospace')
        .text('low');
      svg.append('text').attr('x', lx + lw).attr('y', ly + lh + 8)
        .attr('font-size', 6).attr('fill', '#334155').attr('font-family', 'monospace')
        .attr('text-anchor', 'end').text('high');
    }
    // ── End heat legend ───────────────────────────────────────────────────────

    // Compute max key count for heat fill
    const maxKeys = Math.max(1, ...nodes.map((n) => n.keyCount));

    // Node circles
    nodes.forEach((node) => {
      const pos = nodePositions.get(node.id);
      if (!pos) return;
      const [nx, ny] = pos;
      const isSelected = node.id === selectedNode;
      const isReplicaHighlight = highlightedReplicas.has(node.id);
      const color = nodeColor(node);

      const nodeG = g.append('g')
        .attr('transform', `translate(${nx},${ny})`)
        .attr('cursor', 'pointer')
        .on('click', () => {
          setSelectedNode(node.id);
          navigate(`/nodes/${node.id}`);
        });

      // Replica highlight glow ring (amber, pulsing via CSS animation)
      if (isReplicaHighlight) {
        nodeG.append('circle')
          .attr('r', 22)
          .attr('fill', '#f59e0b')
          .attr('fill-opacity', 0.15)
          .attr('stroke', '#f59e0b')
          .attr('stroke-width', 2)
          .attr('opacity', 0.7);
      }

      // Selection ring
      if (isSelected) {
        nodeG.append('circle')
          .attr('r', 18)
          .attr('fill', 'none')
          .attr('stroke', '#fff')
          .attr('stroke-width', 1.5)
          .attr('opacity', 0.6);
      }

      // Main circle: heat fill (key density) + status border
      const heatFill = node.status === 'crashed' ? '#374151'
        : node.status === 'unreachable' ? '#450a0a'
        : nodeHeatFill(node.keyCount, maxKeys);

      nodeG.append('circle')
        .attr('r', 12)
        .attr('fill', heatFill)
        .attr('fill-opacity', 0.92)
        .attr('stroke', isSelected ? '#e2e8f0' : isReplicaHighlight ? '#f59e0b' : color)
        .attr('stroke-width', isSelected ? 2.5 : isReplicaHighlight ? 2.5 : 1.5);

      // Label
      nodeG.append('text')
        .attr('dy', 24)
        .attr('text-anchor', 'middle')
        .attr('font-size', 9)
        .attr('fill', '#94a3b8')
        .attr('font-family', 'monospace')
        .text(node.id.slice(0, 6));

      // Tooltip
      nodeG.append('title')
        .text(`${node.addr}\nID: ${node.id.slice(0, 12)}...\nKeys: ${node.keyCount}\nStatus: ${node.status}`);
    });

  }, [nodes, keys, nodePositions, selectedNode, showFingers, width, height, cx, cy, ringRadius,
      setSelectedNode, navigate, activeLookup, highlightedKeyId, highlightedReplicas]);

  return (
    <svg
      ref={svgRef}
      width={width}
      height={height}
      style={{ display: 'block', background: '#0a0a0f' }}
    />
  );
}
