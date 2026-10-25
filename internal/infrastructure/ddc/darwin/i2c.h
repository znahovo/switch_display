#ifndef SD_I2C_H
#define SD_I2C_H

#include "registry.h"

// DDC/CI 的 I2C 从地址：标准通道 0x51，LG 的备用通道 0x50。
#define SD_DDC_INPUT_ADDRESS_DEFAULT 0x51
#define SD_DDC_INPUT_ADDRESS_ALT 0x50

// 各平台的等待参数（微秒），移植自 m1ddc。
#define SD_DDC_WAIT 10000
#define SD_DDC_ITERATIONS 2
#define SD_DDC_MCDP_READ_WAIT 50000
#define SD_DDC_BUFFER_SIZE 256

// sd_ddc_read 读取一个 VCP 代码。
//
// 结果通过 outCurrent / outMax 返回；*outSupported 表示显示器是否给了可信回复。
// 返回 0 表示通信成功（即使 *outSupported == 0），非 0 是 IOReturn 错误码。
int sd_ddc_read(SD_IOAVServiceRef service, uint32_t chipAddress, uint8_t vcpCode,
                int useAltAddress, uint16_t *outCurrent, uint16_t *outMax,
                int *outSupported, uint8_t *outRaw, int outRawSize);

// sd_ddc_write 写入一个 VCP 代码。返回 0 表示成功，非 0 是 IOReturn 错误码。
int sd_ddc_write(SD_IOAVServiceRef service, uint32_t chipAddress,
                 uint8_t vcpCode, uint16_t value, int useAltAddress);

#endif // SD_I2C_H
