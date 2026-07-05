import React from 'react';
import { OrderBook as OrderBookType } from '../types';
import { useTheme } from '../contexts/ThemeContext';
import { useCandleColor } from '../contexts/CandleColorContext';

interface OrderBookProps {
  data: OrderBookType;
  compact?: boolean;
  levels?: number;
  className?: string;
}

export const OrderBook: React.FC<OrderBookProps> = ({
  data,
  compact = false,
  levels,
  className = '',
}) => {
  const { colors } = useTheme();
  const cc = useCandleColor();
  // 安全检查：确保 data 及其属性存在
  const bids = data?.bids ?? [];
  const asks = data?.asks ?? [];
  const levelCount = Math.max(1, Math.min(15, levels ?? (compact ? 3 : 15)));

  // 计算委比：(委买量 - 委卖量) / (委买量 + 委卖量) * 100%
  const totalBidSize = bids.reduce((sum, b) => sum + b.size, 0);
  const totalAskSize = asks.reduce((sum, a) => sum + a.size, 0);
  const totalSize = totalBidSize + totalAskSize;

  const weibi = totalSize > 0
    ? ((totalBidSize - totalAskSize) / totalSize * 100).toFixed(2)
    : '0.00';
  const weibiBuy = totalSize > 0 ? (totalBidSize / totalSize * 100).toFixed(1) : '0';
  const weibiSell = totalSize > 0 ? (totalAskSize / totalSize * 100).toFixed(1) : '0';

  if (compact) {
    return (
      <div className={`h-full w-full fin-panel-soft border fin-divider rounded-md overflow-hidden text-[10px] font-mono select-none ${className}`}>
        <div className="h-full grid grid-cols-[minmax(116px,1fr)_64px_minmax(116px,1fr)]">
          <div className="px-2 py-1 border-r fin-divider">
            <div className={`mb-0.5 text-center text-[9px] leading-3 ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>买盘</div>
            {bids.slice(0, levelCount).map((bid, i) => (
              <div key={`compact-bid-${i}`} className="grid grid-cols-[22px_56px_1fr] items-center gap-0.5 leading-4">
                <span className={`text-left ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>{`买${i + 1}`}</span>
                <span className={`text-center tabular-nums ${cc.downClass}`}>{bid.price.toFixed(2)}</span>
                <span className={`text-right tabular-nums ${colors.isDark ? 'text-slate-300' : 'text-slate-600'}`}>{bid.size}</span>
              </div>
            ))}
          </div>

          <div className={`px-1 py-1 border-r fin-divider fin-panel-strong flex flex-col items-center justify-center leading-4 ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>
            <span>委比</span>
            <span className={`font-bold tabular-nums ${parseFloat(weibi) >= 0 ? cc.upClass : cc.downClass}`}>{weibi}%</span>
            <span className="whitespace-nowrap tabular-nums">
              <span className={cc.upClass}>{weibiBuy}</span>/<span className={cc.downClass}>{weibiSell}</span>
            </span>
          </div>

          <div className="px-2 py-1">
            <div className={`mb-0.5 text-center text-[9px] leading-3 ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>卖盘</div>
            {asks.slice(0, levelCount).map((ask, i) => (
              <div key={`compact-ask-${i}`} className="grid grid-cols-[22px_56px_1fr] items-center gap-0.5 leading-4">
                <span className={`text-left ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>{`卖${i + 1}`}</span>
                <span className={`text-center tabular-nums ${cc.upClass}`}>{ask.price.toFixed(2)}</span>
                <span className={`text-right tabular-nums ${colors.isDark ? 'text-slate-300' : 'text-slate-600'}`}>{ask.size}</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className={`h-full flex flex-row fin-panel border-l fin-divider overflow-hidden text-xs font-mono select-none ${className}`}>
       {/* 买盘 */}
       <div className="flex-1 flex flex-col border-r fin-divider">
          <div className={`p-2 border-b fin-divider font-bold flex justify-between fin-panel-strong ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`}>
             <span>买盘</span>
             <span className="text-[10px] font-normal opacity-70">数量</span>
          </div>
          <div className="flex-1 overflow-hidden">
             {bids.slice(0, levelCount).map((bid, i) => (
                <div key={`bid-${i}`} className={`relative flex justify-between px-2 py-0.5 cursor-crosshair ${colors.isDark ? 'hover:bg-slate-800/50' : 'hover:bg-slate-100/50'}`}>
                   <div
                    className={`absolute top-0 left-0 bottom-0 transition-all duration-300 ${colors.isDark ? 'bg-green-900/20' : 'bg-green-500/10'}`}
                    style={{ width: `${Math.min(bid.percent * 5, 100)}%` }}
                  />
                  <span className={`${cc.downClass} relative z-10`}>{bid.price.toFixed(2)}</span>
                  <span className={`relative z-10 ${colors.isDark ? 'text-slate-300' : 'text-slate-600'}`}>{bid.size}</span>
                </div>
             ))}
          </div>
       </div>

       {/* 委比信息 */}
       <div className="w-24 flex flex-col items-center justify-center border-r fin-divider fin-panel-strong z-10 shadow-inner">
           <div className={`text-[10px] ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>委比</div>
           <div className={`font-bold my-1 ${parseFloat(weibi) >= 0 ? cc.upClass : cc.downClass}`}>{weibi}%</div>
           <div className={`text-[10px] ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>
             <span className={cc.upClass}>{weibiBuy}%</span>
             <span className="mx-1">/</span>
             <span className={cc.downClass}>{weibiSell}%</span>
           </div>
       </div>

       {/* 卖盘 */}
       <div className="flex-1 flex flex-col">
          <div className={`p-2 border-b fin-divider font-bold flex justify-between fin-panel-strong ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`}>
             <span>卖盘</span>
             <span className="text-[10px] font-normal opacity-70">数量</span>
          </div>
          <div className="flex-1 overflow-hidden">
            {asks.slice(0, levelCount).map((ask, i) => (
                <div key={`ask-${i}`} className={`relative flex justify-between px-2 py-0.5 cursor-crosshair ${colors.isDark ? 'hover:bg-slate-800/50' : 'hover:bg-slate-100/50'}`}>
                   <div
                    className={`absolute top-0 right-0 bottom-0 transition-all duration-300 ${colors.isDark ? 'bg-red-900/20' : 'bg-red-500/10'}`}
                    style={{ width: `${Math.min(ask.percent * 5, 100)}%` }}
                  />
                  <span className={`${cc.upClass} relative z-10`}>{ask.price.toFixed(2)}</span>
                  <span className={`relative z-10 ${colors.isDark ? 'text-slate-300' : 'text-slate-600'}`}>{ask.size}</span>
                </div>
            ))}
          </div>
       </div>
    </div>
  );
};
