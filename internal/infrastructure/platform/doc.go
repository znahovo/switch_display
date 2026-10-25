// Package platform 提供 application 层端口在 Wails 宿主上的实现。
//
// 这一层很薄：每个文件只做「把一个端口方法转发给 Wails/系统调用」，
// 不含任何判断逻辑。它的存在意义是让 application/domain 不必 import Wails。
package platform
