import React from 'react';

interface SparklineProps {
  values: number[];
  width?: number;
  height?: number;
  positive?: boolean; // 涨=true(红) 跌=false(绿)，由调用方按红涨绿跌传入
}

/** 迷你走势缩略图（近N日收盘），无坐标轴，纯折线 + 渐变填充。 */
export const Sparkline: React.FC<SparklineProps> = ({ values, width = 64, height = 20, positive = true }) => {
  if (!values || values.length < 2) {
    return <svg width={width} height={height} aria-hidden />;
  }
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min || 1;
  const stepX = width / (values.length - 1);
  const y = (v: number) => height - 2 - ((v - min) / span) * (height - 4);
  const pts = values.map((v, i) => `${(i * stepX).toFixed(1)},${y(v).toFixed(1)}`);
  const line = `M ${pts.join(' L ')}`;
  const area = `${line} L ${width},${height} L 0,${height} Z`;
  const color = positive ? '#ef4444' : '#22c55e';
  const gid = `spark-${positive ? 'up' : 'dn'}`;

  return (
    <svg width={width} height={height} className="block">
      <defs>
        <linearGradient id={gid} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity="0.28" />
          <stop offset="100%" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={area} fill={`url(#${gid})`} />
      <path d={line} fill="none" stroke={color} strokeWidth="1.2" strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
};

export default Sparkline;
