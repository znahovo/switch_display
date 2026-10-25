#include "i2c.h"

#include <string.h>
#include <unistd.h>

// 私有符号，与本文件外部的 registry.m 共享声明（registry.h 里已 extern）。
extern IOReturn IOAVServiceReadI2C(SD_IOAVServiceRef service,
                                   uint32_t chipAddress, uint32_t offset,
                                   void *outputBuffer,
                                   uint32_t outputBufferSize);
extern IOReturn IOAVServiceWriteI2C(SD_IOAVServiceRef service,
                                    uint32_t chipAddress, uint32_t dataAddress,
                                    void *inputBuffer,
                                    uint32_t inputBufferSize);

// 包尾长度用「最后一个非零字节的位置 + 1」，与 m1ddc 的 getBytesUsed 一致。
// 这很重要：多余的全零字节会让部分显示器拒收。
static int bytes_used(const uint8_t *data, int size) {
  int used = 0;
  for (int i = 0; i < size; i++) {
    if (data[i] != 0) {
      used = i + 1;
    }
  }
  return used;
}

static uint32_t input_address(int useAltAddress) {
  return useAltAddress ? SD_DDC_INPUT_ADDRESS_ALT
                       : SD_DDC_INPUT_ADDRESS_DEFAULT;
}

int sd_ddc_write(SD_IOAVServiceRef service, uint32_t chipAddress,
                 uint8_t vcpCode, uint16_t value, int useAltAddress) {
  if (service == NULL) {
    return -1;
  }
  uint32_t addr = input_address(useAltAddress);

  // 0x84 = Set VCP Feature，0x03 = 长度，后两字节是 16 位大端值。
  uint8_t data[SD_DDC_BUFFER_SIZE];
  memset(data, 0, sizeof(data));
  data[0] = 0x84;
  data[1] = 0x03;
  data[2] = vcpCode;
  data[3] = (uint8_t)(value >> 8);
  data[4] = (uint8_t)(value & 0xFF);
  data[5] = (uint8_t)(0x6E ^ addr ^ data[0] ^ data[1] ^ data[2] ^ data[3] ^
                      data[4]);

  int len = bytes_used(data, SD_DDC_BUFFER_SIZE);
  IOReturn ret = kIOReturnError;
  for (int i = 0; i < SD_DDC_ITERATIONS; i++) {
    usleep(SD_DDC_WAIT);
    ret = IOAVServiceWriteI2C(service, chipAddress, addr, data, (uint32_t)len);
    if (ret != kIOReturnSuccess) {
      return (int)ret;
    }
  }
  return (int)ret;
}

int sd_ddc_read(SD_IOAVServiceRef service, uint32_t chipAddress, uint8_t vcpCode,
                int useAltAddress, uint16_t *outCurrent, uint16_t *outMax,
                int *outSupported, uint8_t *outRaw, int outRawSize) {
  if (outSupported != NULL) {
    *outSupported = 0;
  }
  if (service == NULL) {
    return -1;
  }
  uint32_t addr = input_address(useAltAddress);

  // 0x82 = Get VCP Feature。
  //
  // 校验和必须把 inputAddr 也 XOR 进去。m1ddc 的 prepareDDCRead 漏了这一步
  // （它算的是 0x6e ^ data[0..2] ^ data[3]=0），在本机实测会导致显示器
  // 回一帧纯噪声（例如读 contrast 回来是 50739 而不是 50）。加上 address
  // 后回复帧才是规整的 `6e 88 02 00 <vcp> 00 00 <max> 00 <cur> 00 18`。
  uint8_t request[SD_DDC_BUFFER_SIZE];
  memset(request, 0, sizeof(request));
  request[0] = 0x82;
  request[1] = 0x01;
  request[2] = vcpCode;
  request[3] = (uint8_t)(0x6E ^ addr ^ request[0] ^ request[1] ^ request[2]);

  usleep(SD_DDC_WAIT);
  IOReturn ret =
      IOAVServiceWriteI2C(service, chipAddress, addr, request, 4);
  if (ret != kIOReturnSuccess) {
    return (int)ret;
  }

  // MCDP29xx 回复更慢，m1ddc 用 50ms。
  usleep(chipAddress == SD_DDC_CHIP_ADDRESS_MCDP29XX ? SD_DDC_MCDP_READ_WAIT
                                                     : SD_DDC_WAIT);
  uint8_t reply[12];
  memset(reply, 0, sizeof(reply));
  ret = IOAVServiceReadI2C(service, chipAddress, addr, reply, 12);
  if (ret != kIOReturnSuccess) {
    return (int)ret;
  }
  if (outRaw != NULL && outRawSize > 0) {
    int n = outRawSize < 12 ? outRawSize : 12;
    memcpy(outRaw, reply, (size_t)n);
  }

  // 校验回复帧：0x6E 源地址 / 0x02 Get VCP Feature Reply / result code 0。
  if (reply[0] != 0x6E || reply[2] != 0x02 || reply[3] != 0x00) {
    return 0; // 通信成功但没有有效回复 → supported=0
  }
  if (reply[4] != vcpCode) {
    return 0;
  }

  uint16_t maxValue = (uint16_t)((reply[6] << 8) | reply[7]);
  uint16_t curValue = (uint16_t)((reply[8] << 8) | reply[9]);

  if (outMax != NULL) {
    *outMax = maxValue;
  }
  if (outCurrent != NULL) {
    *outCurrent = curValue;
  }

  // 可信度判断。
  //
  // 连续量（亮度/对比度）必须满足 cur <= max；但输入源（0x60/0xF4）的 max
  // 字段在实测中被显示器填成 14 这种无意义的值，只能要求 cur 非零。
  int plausible;
  if (vcpCode == 0x60 || vcpCode == 0xF4) {
    plausible = curValue != 0;
  } else {
    plausible = (maxValue != 0) && (curValue <= maxValue);
  }
  if (outSupported != NULL) {
    *outSupported = plausible ? 1 : 0;
  }
  return 0;
}
