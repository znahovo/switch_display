#ifndef SD_REGISTRY_H
#define SD_REGISTRY_H

#include <CoreFoundation/CoreFoundation.h>
#include <CoreGraphics/CoreGraphics.h>
#include <IOKit/IOKitLib.h>

// 单台 Mac 上同时在线显示器上限。m1ddc 用 4，这里放宽到 16，
// 避免多口扩展坞场景被静默截断。
#define SD_MAX_DISPLAYS 16

// MCDP29xx 走 0xB7，其余走 0x37（见 m1ddc ioregistry.h）。
#define SD_DDC_CHIP_ADDRESS_DEFAULT 0x37
#define SD_DDC_CHIP_ADDRESS_MCDP29XX 0xB7

// IOAVServiceRef 是私有类型，只能自己 typedef 成 CFTypeRef。
typedef CFTypeRef SD_IOAVServiceRef;

// SDDisplayRecord 是一次枚举里的一台显示器。
//
// 字符串是定长 C 数组，避免跨 cgo 边界传 CFStringRef 的所有权问题。
// adapter 是 IOKit 的 io_service_t，用完必须 sd_release_displays。
typedef struct {
  CGDirectDisplayID id;
  io_service_t adapter;
  unsigned int vendorNum;
  unsigned int modelNum;
  unsigned int serialNum;
  int builtin;
  char uuid[64];
  char name[256];
  char edid[64];
  char anSerial[64];
  char manufacturer[64];
} SDDisplayRecord;

// sd_list_displays 枚举在线显示器，写入 out（最多 capacity 个），返回实际数量；
// 失败返回负的错误码。
int sd_list_displays(SDDisplayRecord *out, int capacity);

// sd_release_displays 释放 sd_list_displays 返回的记录里持有的 IOKit 对象。
void sd_release_displays(SDDisplayRecord *records, int count);

// sd_open_transport 为给定 adapter 建立 DDC 通道。
// 成功返回 IOAVServiceRef（调用方负责 CFRelease）并写入 chipAddress；
// 失败返回 NULL。
SD_IOAVServiceRef sd_open_transport(io_service_t adapter,
                                    uint32_t *chipAddress);

// sd_default_service 返回兜底的 AV service（拿不到具体显示器时用）。
SD_IOAVServiceRef sd_default_service(void);

// --- 私有 API（与 m1ddc 相同，通过动态符号解析） ---

extern SD_IOAVServiceRef IOAVServiceCreate(CFAllocatorRef allocator);
extern SD_IOAVServiceRef IOAVServiceCreateWithService(CFAllocatorRef allocator,
                                                      io_service_t service);
extern CFDictionaryRef CoreDisplay_DisplayCreateInfoDictionary(CGDirectDisplayID);
extern IOReturn IOAVServiceReadI2C(SD_IOAVServiceRef service, uint32_t chipAddress,
                                   uint32_t offset, void *outputBuffer,
                                   uint32_t outputBufferSize);
extern IOReturn IOAVServiceWriteI2C(SD_IOAVServiceRef service, uint32_t chipAddress,
                                    uint32_t dataAddress, void *inputBuffer,
                                    uint32_t inputBufferSize);

#endif // SD_REGISTRY_H
