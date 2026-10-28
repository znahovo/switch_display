// 虚拟滚动边界计算的测试。
//
// 为什么值得测：这段逻辑只在「上千条 + 折行」时才暴露问题，而在浏览器里
// 手测很难稳定复现「差一行」。用 node 直接跑（Node 22+ 默认剥类型）。
import assert from "node:assert/strict"
import { test } from "node:test"
import { buildEdges, findRange } from "./virtual.ts"

test("buildEdges 是前缀和，首项为 0、末项为总高", () => {
  const { edges, total } = buildEdges(4, () => 10)
  assert.deepEqual(edges, [0, 10, 20, 30, 40])
  assert.equal(total, 40)
})

test("buildEdges 对 0 行也给出合法的单元素表", () => {
  const { edges, total } = buildEdges(0, () => 10)
  assert.deepEqual(edges, [0])
  assert.equal(total, 0)
})

test("buildEdges 不接受 0 或负的高度（会退化成 1）", () => {
  const { total } = buildEdges(3, () => 0)
  assert.equal(total, 3)
})

test("空列表返回空区间，调用方不必特判", () => {
  assert.deepEqual(findRange([0], 0, 0, 100, 6), { start: 0, end: 0, offset: 0 })
})

test("停在顶部时只渲染视口 + overscan", () => {
  const { edges } = buildEdges(1000, () => 20)
  // 视口 200px = 10 行；overscan 6 → start 0、end 16（10 + 6）
  const r = findRange(edges, 1000, 0, 200, 6)
  assert.equal(r.start, 0)
  assert.equal(r.end, 16)
  assert.equal(r.offset, 0)
})

test("滚到中间时首行下标与位移正确", () => {
  const { edges } = buildEdges(1000, () => 20)
  // scrollTop 500 → 第 25 行是首行；overscan 6 → start 19
  const r = findRange(edges, 1000, 500, 200, 6)
  assert.equal(r.start, 19)
  assert.equal(r.offset, 19 * 20)
  // 视口覆盖 25..34 行，尾部再加 overscan
  assert.equal(r.end, 41)
})

test("滚到底部时 end 正好停在条目数，不越界", () => {
  const { edges, total } = buildEdges(50, () => 20)
  const r = findRange(edges, 50, total - 200, 200, 6)
  assert.equal(r.end, 50)
  assert.ok(r.start < 50)
})

test("scrollTop 超过最大可滚距离时被夹住（否则会渲染空区间）", () => {
  const { edges } = buildEdges(50, () => 20)
  const r = findRange(edges, 50, 99999, 200, 6)
  assert.equal(r.end, 50)
  assert.ok(r.end - r.start >= 10)
})

test("内容比视口短时不滚动，仍渲染全部", () => {
  const { edges } = buildEdges(3, () => 20)
  const r = findRange(edges, 3, 0, 800, 6)
  assert.equal(r.start, 0)
  assert.equal(r.end, 3)
})

test("行高不一致时依据前缀和定位，而不是等差数列", () => {
  // 第 0 行特别高（折行），后面都很矮
  const heights = [100, 20, 20, 20, 20, 20]
  const { edges } = buildEdges(heights.length, (i) => heights[i])
  // scrollTop 100 刚好越过第一行 → 首行是下标 1
  const r = findRange(edges, heights.length, 100, 40, 0)
  assert.equal(r.start, 1)
  assert.equal(r.offset, 100)
})

test("overscan 大于条目数时不会出现负下标", () => {
  const { edges } = buildEdges(3, () => 20)
  const r = findRange(edges, 3, 0, 40, 50)
  assert.equal(r.start, 0)
  assert.equal(r.end, 3)
})
