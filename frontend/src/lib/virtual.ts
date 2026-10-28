// 虚拟滚动的纯计算部分。
//
// 单独拆出来是为了能脱离 DOM 验证：二分边界差一行，表现就是滚动条
// 抖一下或者底部露出空白，而这种问题在浏览器里手测很难稳定复现。

/** 每行的偏移表：edges[i] 是第 i 行的顶部，edges[n] 是总高度。 */
export function buildEdges(
  count: number,
  heightOf: (i: number) => number,
): { edges: number[]; total: number } {
  const edges = new Array<number>(count + 1)
  edges[0] = 0
  for (let i = 0; i < count; i++) {
    edges[i + 1] = edges[i] + Math.max(1, heightOf(i))
  }
  return { edges, total: edges[count] }
}

export interface Range {
  /** 第一行在 items 里的下标（含 overscan）。 */
  start: number
  /** 最后一行的下标 +1（含 overscan）。 */
  end: number
  /** 这个切片相对列表顶部的位移。 */
  offset: number
}

/**
 * 算出当前视口要渲染哪一段。
 *
 * 约定：返回的 start/end 已经算上 overscan，并夹在 [0, count] 内；
 * 列表为空时返回 {0,0,0}，调用方不必特判。
 */
export function findRange(
  edges: number[],
  count: number,
  scrollTop: number,
  viewport: number,
  overscan: number,
): Range {
  if (count === 0) return { start: 0, end: 0, offset: 0 }

  const total = edges[count]
  const height = Math.max(1, viewport)
  // 内容比视口还短时不滚，否则 scrollTop 可能是个残留值。
  const top = Math.max(0, Math.min(scrollTop, Math.max(0, total - height)))

  // 找最后一个 edges[i] <= top 的行：它就是视口第一行。
  let lo = 0
  let hi = count
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1
    if (edges[mid] <= top) lo = mid
    else hi = mid - 1
  }

  let end = lo
  while (end < count && edges[end] < top + height) end++

  const start = Math.max(0, lo - overscan)
  return {
    start,
    end: Math.min(count, end + overscan),
    offset: edges[start],
  }
}

/** 视口第一行的下标（不含 overscan）：用来做「滚动锚点」。 */
export function topIndex(
  edges: number[],
  count: number,
  scrollTop: number,
  viewport: number,
): number {
  return findRange(edges, count, scrollTop, viewport, 0).start
}

/**
 * 由「锚点行的新位置 + 锚点行内的偏移」算出新的 scrollTop。
 *
 * 用途：日志从头部被裁掉时，内容整体上移，用户正在看的那行会突然跳走。
 * 记住它原来的相对位置，裁完按新位置把视口拉回去，视觉上就不动了。
 */
export function anchoredScrollTop(
  edges: number[],
  index: number,
  intraOffset: number,
): number {
  if (index < 0 || index >= edges.length) return 0
  return Math.max(0, edges[index] + intraOffset)
}
