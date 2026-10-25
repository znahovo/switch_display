import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

/** cn 合并类名：shadcn-vue 的所有组件的标准入口。 */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
