import React, { useState, useEffect } from 'react';
import { X, Briefcase } from 'lucide-react';
import type { StockPosition } from '../types';
import { useTheme } from '../contexts/ThemeContext';
import { useCandleColor } from '../contexts/CandleColorContext';
import { reportToBackend } from '../utils/reportToBackend';

interface PositionDialogProps {
  isOpen: boolean;
  onClose: () => void;
  stockCode: string;
  stockName: string;
  currentPrice: number;
  position?: StockPosition;
  onSave: (shares: number, costPrice: number, buyDate: string) => void;
  onSell?: (sellPrice: number) => void;
  onReduce?: (sellShares: number, sellPrice: number) => void;
  /** 该股已实现盈亏(历次卖出/减仓累计)。没有就传 0。 */
  realizedPnL?: number;
}

export const PositionDialog: React.FC<PositionDialogProps> = ({
  isOpen,
  onClose,
  stockCode,
  stockName,
  currentPrice,
  position,
  onSave,
  onSell,
  onReduce,
  realizedPnL = 0,
}) => {
  const { colors } = useTheme();
  const cc = useCandleColor();
  const [shares, setShares] = useState<string>('');
  const [costPrice, setCostPrice] = useState<string>('');
  const [buyDate, setBuyDate] = useState<string>('');
  const [sellPrice, setSellPrice] = useState<string>('');
  const [reduceShares, setReduceShares] = useState<string>('');

  useEffect(() => {
    if (isOpen && position) {
      setShares(position.shares > 0 ? position.shares.toString() : '');
      setCostPrice(position.costPrice > 0 ? position.costPrice.toString() : '');
      setBuyDate(position.buyDate || '');
      setReduceShares('');
    } else if (isOpen) {
      setShares('');
      setCostPrice('');
      setBuyDate('');
    }
  }, [isOpen, position]);

  if (!isOpen) return null;

  const sharesNum = parseInt(shares) || 0;
  const costPriceNum = parseFloat(costPrice) || 0;
  const costAmount = sharesNum * costPriceNum;
  const marketValue = sharesNum * currentPrice;
  const profitLoss = marketValue - costAmount;
  const profitLossPercent = costAmount > 0 ? (profitLoss / costAmount) * 100 : 0;

  const handleSave = () => {
    onSave(sharesNum, costPriceNum, buyDate);
    onClose();
  };

  const handleClear = () => {
    onSave(0, 0, '');
    onClose();
  };

  const handleReduce = () => {
    const n = parseInt(reduceShares) || 0;
    const sp = parseFloat(sellPrice) || currentPrice;
    reportToBackend('减仓', `按钮点击 n=${n} 价=${sp} onReduce=${typeof onReduce}`);
    if (n <= 0) { reportToBackend('减仓', '❌ 数量为 0,已忽略'); return; }
    if (!onReduce) { reportToBackend('减仓', '❌ onReduce 未传入(接线问题)'); return; }
    try {
      onReduce(n, sp);
    } catch (e) {
      reportToBackend('减仓', `❌ onReduce 抛异常:${e instanceof Error ? e.message : String(e)}`);
    }
    onClose();
  };

  const handleSell = () => {
    const sp = parseFloat(sellPrice) || currentPrice;
    if (onSell) onSell(sp);
    else onSave(0, 0, '');
    onClose();
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60" onClick={onClose} />
      <div className="relative w-96 fin-panel border fin-divider rounded-xl shadow-2xl">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b fin-divider">
          <div className="flex items-center gap-2">
            <Briefcase className="h-5 w-5 text-accent-2" />
            <span className={`font-bold ${colors.isDark ? 'text-slate-100' : 'text-slate-800'}`}>持仓设置</span>
          </div>
          <button
            onClick={onClose}
            className={`p-1 rounded transition-colors ${colors.isDark ? 'hover:bg-slate-700 text-slate-400 hover:text-white' : 'hover:bg-slate-200 text-slate-500 hover:text-slate-700'}`}
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Stock Info */}
        <div className={`px-4 py-3 border-b fin-divider ${colors.isDark ? 'bg-slate-800/30' : 'bg-slate-100/50'}`}>
          <div className="flex justify-between items-center">
            <div>
              <span className={`font-medium ${colors.isDark ? 'text-slate-100' : 'text-slate-800'}`}>{stockName}</span>
              <span className={`ml-2 text-sm font-mono ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`}>{stockCode}</span>
            </div>
            <span className="text-lg font-mono text-accent-2">{currentPrice.toFixed(2)}</span>
          </div>
        </div>

        {/* Form */}
        <div className="p-4 space-y-4 text-left">
          <div>
            <label className={`block text-sm mb-1 ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`}>持仓数量（股）</label>
            <input
              type="number"
              value={shares}
              onChange={(e) => setShares(e.target.value)}
              placeholder="请输入持仓数量"
              className="w-full fin-input rounded-lg px-3 py-2 text-sm"
              min="0"
              step="100"
            />
            {position && position.shares > 0 && (parseInt(shares) || 0) > 0 && (parseInt(shares) || 0) < position.shares && (
              <div className="mt-1 text-xs text-amber-500">
                ⚠️ 改小数量只是<b>更正记录</b>，不记台账、不算已实现盈亏。真卖了请用下面的「减仓」。
              </div>
            )}
          </div>
          <div>
            <label className={`block text-sm mb-1 ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`}>成本价（元）</label>
            <input
              type="number"
              value={costPrice}
              onChange={(e) => setCostPrice(e.target.value)}
              placeholder="请输入成本价"
              className="w-full fin-input rounded-lg px-3 py-2 text-sm"
              min="0"
              step="0.01"
            />
          </div>
          <div>
            <label className={`block text-sm mb-1 ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`}>买入日期（用于持仓天数/时间止损，可选）</label>
            <input
              type="date"
              value={buyDate}
              onChange={(e) => setBuyDate(e.target.value)}
              className="w-full fin-input rounded-lg px-3 py-2 text-sm"
            />
          </div>

          {/* Calculated Info */}
          {sharesNum > 0 && costPriceNum > 0 && (
            <div className={`p-3 rounded-lg space-y-2 text-sm ${colors.isDark ? 'bg-slate-800/50' : 'bg-slate-100'}`}>
              <div className="flex justify-between">
                <span className={colors.isDark ? 'text-slate-400' : 'text-slate-500'}>成本金额</span>
                <span className={`font-mono ${colors.isDark ? 'text-slate-200' : 'text-slate-700'}`}>{costAmount.toFixed(2)}</span>
              </div>
              <div className="flex justify-between">
                <span className={colors.isDark ? 'text-slate-400' : 'text-slate-500'}>市值</span>
                <span className={`font-mono ${colors.isDark ? 'text-slate-200' : 'text-slate-700'}`}>{marketValue.toFixed(2)}</span>
              </div>
              <div className="flex justify-between">
                <span className={colors.isDark ? 'text-slate-400' : 'text-slate-500'}>浮动盈亏</span>
                <span className={`font-mono ${cc.getColorClass(profitLoss >= 0)}`}>
                  {profitLoss >= 0 ? '+' : ''}{profitLoss.toFixed(2)} ({profitLossPercent >= 0 ? '+' : ''}{profitLossPercent.toFixed(2)}%)
                </span>
              </div>
              {/* 已实现单独一行:减仓卖掉那部分的利润不在浮盈里,不显示的话用户会以为钱少了 */}
              {Math.abs(realizedPnL) > 0.005 && (
                <>
                  <div className="flex justify-between">
                    <span className={colors.isDark ? 'text-slate-400' : 'text-slate-500'}>已实现</span>
                    <span className={`font-mono ${cc.getColorClass(realizedPnL >= 0)}`}>
                      {realizedPnL >= 0 ? '+' : ''}{realizedPnL.toFixed(2)}
                    </span>
                  </div>
                  <div className="flex justify-between pt-1 border-t fin-divider">
                    <span className={colors.isDark ? 'text-slate-300' : 'text-slate-600'}>合计</span>
                    <span className={`font-mono font-bold ${cc.getColorClass(profitLoss + realizedPnL >= 0)}`}>
                      {profitLoss + realizedPnL >= 0 ? '+' : ''}{(profitLoss + realizedPnL).toFixed(2)}
                    </span>
                  </div>
                </>
              )}
            </div>
          )}
        </div>

        {/* 卖出区（持仓时显示，记入交易台账） */}
        {position && position.shares > 0 && (
          <div className={`mx-4 mb-2 p-3 rounded-lg border ${colors.isDark ? 'border-slate-700 bg-slate-800/30' : 'border-slate-200 bg-slate-50'}`}>
            <div className="flex items-center gap-2">
              <span className={`text-sm ${colors.isDark ? 'text-slate-300' : 'text-slate-600'}`}>卖出价</span>
              <input
                type="number"
                value={sellPrice}
                onChange={(e) => setSellPrice(e.target.value)}
                placeholder={`默认现价 ${currentPrice.toFixed(2)}`}
                className="flex-1 fin-input rounded-lg px-3 py-1.5 text-sm"
                step="0.01"
              />
              <button
                onClick={handleSell}
                className="px-4 py-1.5 rounded-lg text-sm text-white bg-red-500/80 hover:bg-red-500 transition-colors whitespace-nowrap"
              >
                卖出清仓
              </button>
            </div>
            {onReduce && (() => {
              const rn = parseInt(reduceShares) || 0;
              const sp = parseFloat(sellPrice) || currentPrice;
              const ok = rn > 0 && rn <= position.shares;
              // 快捷按钮:减仓最常见的就是减一半/三分之一,手打整百数容易输错
              const quick = [
                { label: '½ 仓', n: Math.floor(position.shares / 2 / 100) * 100 },
                { label: '⅓ 仓', n: Math.floor(position.shares / 3 / 100) * 100 },
                { label: '全部', n: position.shares },
              ].filter(q => q.n > 0);
              return (
                <div className="mt-2 space-y-2">
                  <div className="flex items-center gap-2">
                    <span className={`text-sm ${colors.isDark ? 'text-slate-300' : 'text-slate-600'}`}>减仓数</span>
                    <input
                      type="number"
                      value={reduceShares}
                      onChange={(e) => setReduceShares(e.target.value)}
                      placeholder={`最多 ${position.shares}`}
                      className="flex-1 fin-input rounded-lg px-3 py-1.5 text-sm"
                      min="0"
                      max={position.shares}
                      step="100"
                    />
                    <button
                      onClick={handleReduce}
                      disabled={!ok}
                      className={`px-4 py-1.5 rounded-lg text-sm whitespace-nowrap transition-colors ${
                        ok
                          ? 'text-white bg-amber-500 hover:bg-amber-400 cursor-pointer'
                          : `cursor-not-allowed ${colors.isDark ? 'bg-slate-700/60 text-slate-500' : 'bg-slate-200 text-slate-400'}`
                      }`}
                    >
                      减仓
                    </button>
                  </div>
                  <div className="flex items-center gap-2">
                    {quick.map(q => (
                      <button
                        key={q.label}
                        onClick={() => setReduceShares(String(q.n))}
                        className={`px-2 py-0.5 rounded text-xs transition-colors ${colors.isDark ? 'bg-slate-700/60 text-slate-300 hover:bg-slate-600' : 'bg-slate-200 text-slate-600 hover:bg-slate-300'}`}
                      >
                        {q.label} {q.n}股
                      </button>
                    ))}
                  </div>
                  {/* 提交前先把结果摆出来:减仓是不可逆动作,按下去才知道减了多少太晚了 */}
                  {ok && costPriceNum > 0 && (
                    <div className={`text-xs ${colors.isDark ? 'text-slate-400' : 'text-slate-500'}`}>
                      卖 {rn} 股 @ {sp.toFixed(2)} → 已实现{' '}
                      <span className={cc.getColorClass(rn * (sp - costPriceNum) >= 0)}>
                        {rn * (sp - costPriceNum) >= 0 ? '+' : ''}{(rn * (sp - costPriceNum)).toFixed(2)}
                      </span>
                      ,剩 {position.shares - rn} 股(成本价仍 {costPriceNum.toFixed(2)})
                    </div>
                  )}
                  {!ok && (
                    <div className={`text-xs ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>
                      {rn > position.shares ? `超过持仓 ${position.shares} 股` : '先填减仓数量,或点上面的快捷按钮'}
                    </div>
                  )}
                </div>
              );
            })()}
            <div className={`text-xs mt-1 ${colors.isDark ? 'text-slate-500' : 'text-slate-400'}`}>
              卖出/减仓都会记入交易台账，卖出价留空按现价。
              <span className="block mt-0.5">减仓后<b>成本价保持不变</b>——止损线按成本价算，摊薄成本会让止损线悄悄挪走。</span>
            </div>
          </div>
        )}

        {/* Footer */}
        <div className="flex gap-2 p-4 border-t fin-divider">
          {position && position.shares > 0 && (
            <button
              onClick={handleClear}
              className="px-4 py-2 rounded-lg text-sm text-slate-400 hover:bg-slate-500/10 transition-colors"
              title="只清空持仓，不记台账"
            >
              仅清空
            </button>
          )}
          <div className="flex-1" />
          <button
            onClick={onClose}
            className={`px-4 py-2 rounded-lg text-sm transition-colors ${colors.isDark ? 'text-slate-400 hover:bg-slate-700' : 'text-slate-500 hover:bg-slate-200'}`}
          >
            取消
          </button>
          <button
            onClick={handleSave}
            className="px-4 py-2 rounded-lg text-sm bg-accent hover:bg-accent text-white transition-colors"
          >
            保存
          </button>
        </div>
      </div>
    </div>
  );
};
