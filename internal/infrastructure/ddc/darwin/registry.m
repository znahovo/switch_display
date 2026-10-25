#include "registry.h"

#include <dlfcn.h>
#include <string.h>

// 私有符号用 dlsym 解析，而不是 extern 直接链接：
//   1. 不需要链接私有框架 CoreDisplay（分发更稳）
//   2. 符号缺失时可以干净地降级成「不支持」，而不是启动即崩
typedef SD_IOAVServiceRef (*fnIOAVServiceCreate)(CFAllocatorRef);
typedef SD_IOAVServiceRef (*fnIOAVServiceCreateWithService)(CFAllocatorRef,
                                                            io_service_t);
typedef CFDictionaryRef (*fnDisplayCreateInfoDictionary)(CGDirectDisplayID);
typedef CFUUIDRef (*fnCreateUUIDFromDisplayID)(CGDirectDisplayID);

static fnIOAVServiceCreate p_IOAVServiceCreate;
static fnIOAVServiceCreateWithService p_IOAVServiceCreateWithService;
static fnDisplayCreateInfoDictionary p_DisplayCreateInfoDictionary;
static fnCreateUUIDFromDisplayID p_CreateUUIDFromDisplayID;
static Boolean g_symbolsReady = false;

static void resolve_symbols(void) {
  if (g_symbolsReady) {
    return;
  }
  g_symbolsReady = true;
  p_IOAVServiceCreate =
      (fnIOAVServiceCreate)dlsym(RTLD_DEFAULT, "IOAVServiceCreate");
  p_IOAVServiceCreateWithService = (fnIOAVServiceCreateWithService)dlsym(
      RTLD_DEFAULT, "IOAVServiceCreateWithService");
  p_DisplayCreateInfoDictionary = (fnDisplayCreateInfoDictionary)dlsym(
      RTLD_DEFAULT, "CoreDisplay_DisplayCreateInfoDictionary");
  // CGDisplayCreateUUIDFromDisplayID 在 SDK 里没有公开声明，同样走 dlsym。
  p_CreateUUIDFromDisplayID =
      (fnCreateUUIDFromDisplayID)dlsym(RTLD_DEFAULT,
                                       "CGDisplayCreateUUIDFromDisplayID");
}

// 从 service 上按 key 递归找 CFProperty。
static CFTypeRef copy_property(io_service_t service, const char *key) {
  CFStringRef cfkey = CFStringCreateWithCString(kCFAllocatorDefault, key,
                                                kCFStringEncodingASCII);
  if (cfkey == NULL) {
    return NULL;
  }
  CFTypeRef value = IORegistryEntrySearchCFProperty(
      service, kIOServicePlane, cfkey, kCFAllocatorDefault,
      kIORegistryIterateRecursively);
  CFRelease(cfkey);
  return value;
}

static void copy_cfstring(CFTypeRef value, char *out, size_t outSize) {
  out[0] = '\0';
  if (value == NULL || CFGetTypeID(value) != CFStringGetTypeID()) {
    return;
  }
  CFStringGetCString((CFStringRef)value, out, (CFIndex)outSize,
                     kCFStringEncodingUTF8);
}

// 递归找第一个满足 predicate 的 service。找到后 *out 由调用方释放。
typedef Boolean (*service_predicate)(io_service_t service, void *ctx);

static io_service_t find_service(service_predicate pred, void *ctx) {
  io_registry_entry_t root = IORegistryGetRootEntry(kIOMainPortDefault);
  if (root == MACH_PORT_NULL) {
    return MACH_PORT_NULL;
  }
  io_iterator_t iter = MACH_PORT_NULL;
  if (IORegistryEntryCreateIterator(root, kIOServicePlane,
                                    kIORegistryIterateRecursively,
                                    &iter) != KERN_SUCCESS) {
    IOObjectRelease(root);
    return MACH_PORT_NULL;
  }
  IOObjectRelease(root);

  io_service_t found = MACH_PORT_NULL;
  io_service_t service;
  while ((service = IOIteratorNext(iter)) != MACH_PORT_NULL) {
    if (pred(service, ctx)) {
      // 保留这个 service 的引用（迭代器下次 Next 不会释放它）
      found = service;
      break;
    }
    IOObjectRelease(service);
  }
  IOObjectRelease(iter);
  return found;
}

// is_mcdp29xx 判断 DCPAVServiceProxy 的父节点是不是 AppleDCPMCDP29XX。
static Boolean is_mcdp29xx(io_service_t proxy) {
  io_registry_entry_t parent = MACH_PORT_NULL;
  if (IORegistryEntryGetParentEntry(proxy, kIOServicePlane, &parent) !=
      KERN_SUCCESS) {
    return false;
  }
  Boolean result = false;
  CFTypeRef providerClass = IORegistryEntryCreateCFProperty(
      parent, CFSTR("EPICProviderClass"), kCFAllocatorDefault, 0);
  if (providerClass != NULL &&
      CFGetTypeID(providerClass) == CFStringGetTypeID()) {
    result = CFStringCompare((CFStringRef)providerClass,
                             CFSTR("AppleDCPMCDP29XX"), 0) ==
             kCFCompareEqualTo;
  }
  if (providerClass != NULL) {
    CFRelease(providerClass);
  }
  IOObjectRelease(parent);
  return result;
}

// 为某个 display 找对应的 DCPAVServiceProxy（Location == External）。
typedef struct {
  uint64_t targetAdapterID;
  Boolean matchedFramebuffer;
} proxy_ctx;

static Boolean proxy_predicate(io_service_t service, void *rawCtx) {
  proxy_ctx *ctx = (proxy_ctx *)rawCtx;

  char name[128];
  name[0] = '\0';
  IORegistryEntryGetName(service, name);
  if (strcmp(name, "IOMobileFramebuffer") == 0) {
    uint64_t fbid = 0;
    if (IORegistryEntryGetRegistryEntryID(service, &fbid) == KERN_SUCCESS) {
      // 迭代顺序里，帧缓冲entry 之后紧跟的 DCPAVServiceProxy 才属于这台显示器。
      ctx->matchedFramebuffer = (fbid == ctx->targetAdapterID);
    }
    return false;
  }

  if (!ctx->matchedFramebuffer || strcmp(name, "DCPAVServiceProxy") != 0) {
    return false;
  }

  CFTypeRef location = copy_property(service, "Location");
  Boolean isExternal = false;
  if (location != NULL && CFGetTypeID(location) == CFStringGetTypeID()) {
    isExternal = CFStringCompare((CFStringRef)location, CFSTR("External"), 0) ==
                 kCFCompareEqualTo;
  }
  if (location != NULL) {
    CFRelease(location);
  }
  return isExternal;
}

int sd_list_displays(SDDisplayRecord *out, int capacity) {
  resolve_symbols();
  if (out == NULL || capacity <= 0) {
    return -1;
  }
  if (p_DisplayCreateInfoDictionary == NULL) {
    // 老系统/受限环境：拿不到 DisplayAttributes，但我们仍可用公开 API 枚举。
    // 这里继续走，只是名字与 EDID 为空。
  }

  CGDirectDisplayID ids[SD_MAX_DISPLAYS];
  CGDisplayCount count = 0;
  if (CGGetOnlineDisplayList(SD_MAX_DISPLAYS, ids, &count) != kCGErrorSuccess) {
    return -2;
  }

  int written = 0;
  for (CGDisplayCount i = 0; i < count && written < capacity; i++) {
    CGDirectDisplayID did = ids[i];

    SDDisplayRecord rec;
    memset(&rec, 0, sizeof(rec));
    rec.id = did;
    rec.vendorNum = CGDisplayVendorNumber(did);
    rec.modelNum = CGDisplayModelNumber(did);
    rec.serialNum = CGDisplaySerialNumber(did);
    rec.builtin = CGDisplayIsBuiltin(did) ? 1 : 0;

    // UUID：优先用私有 info dictionary 的 kCGDisplayUUID（m1ddc 的做法），
    // 退化为公开的 CGDisplayCreateUUIDFromDisplayID。
    CFStringRef uuidStr = NULL;
    if (p_DisplayCreateInfoDictionary != NULL) {
      CFDictionaryRef info = p_DisplayCreateInfoDictionary(did);
      if (info != NULL) {
        CFTypeRef v = CFDictionaryGetValue(info, CFSTR("kCGDisplayUUID"));
        if (v != NULL && CFGetTypeID(v) == CFStringGetTypeID()) {
          uuidStr = (CFStringRef)CFRetain(v);
        }
        CFRelease(info);
      }
    }
    if (uuidStr == NULL && p_CreateUUIDFromDisplayID != NULL) {
      CFUUIDRef uuid = p_CreateUUIDFromDisplayID(did);
      if (uuid != NULL) {
        uuidStr = CFUUIDCreateString(kCFAllocatorDefault, uuid);
        CFRelease(uuid);
      }
    }
    if (uuidStr != NULL) {
      copy_cfstring(uuidStr, rec.uuid, sizeof(rec.uuid));
      CFRelease(uuidStr);
    }
    // 没有 UUID 的虚拟显示器（Sidecar/AirPlay）直接跳过，与 m1ddc 一致。
    if (rec.uuid[0] == '\0') {
      continue;
    }

    // IODisplayLocation 指向 display adapter，用它拿 EDID 与 DDC transport。
    CFStringRef location = NULL;
    if (p_DisplayCreateInfoDictionary != NULL) {
      CFDictionaryRef info = p_DisplayCreateInfoDictionary(did);
      if (info != NULL) {
        CFTypeRef v = CFDictionaryGetValue(info, CFSTR("IODisplayLocation"));
        if (v != NULL && CFGetTypeID(v) == CFStringGetTypeID()) {
          location = (CFStringRef)CFRetain(v);
        }
        CFRelease(info);
      }
    }
    if (location == NULL) {
      continue;
    }
    rec.adapter = IORegistryEntryCopyFromPath(kIOMainPortDefault, location);
    CFRelease(location);
    if (rec.adapter == MACH_PORT_NULL) {
      continue;
    }

    CFTypeRef edid = copy_property(rec.adapter, "EDID UUID");
    copy_cfstring(edid, rec.edid, sizeof(rec.edid));
    if (edid != NULL) {
      CFRelease(edid);
    }

    CFTypeRef attrs = copy_property(rec.adapter, "DisplayAttributes");
    if (attrs != NULL) {
      if (CFGetTypeID(attrs) == CFDictionaryGetTypeID()) {
        CFDictionaryRef product = (CFDictionaryRef)CFDictionaryGetValue(
            (CFDictionaryRef)attrs, CFSTR("ProductAttributes"));
        if (product != NULL &&
            CFGetTypeID(product) == CFDictionaryGetTypeID()) {
          copy_cfstring(CFDictionaryGetValue(product, CFSTR("ProductName")),
                        rec.name, sizeof(rec.name));
          copy_cfstring(CFDictionaryGetValue(product, CFSTR("ManufacturerID")),
                        rec.manufacturer, sizeof(rec.manufacturer));
          copy_cfstring(
              CFDictionaryGetValue(product, CFSTR("AlphanumericSerialNumber")),
              rec.anSerial, sizeof(rec.anSerial));
        }
      }
      CFRelease(attrs);
    }

    out[written++] = rec;
  }
  return written;
}

void sd_release_displays(SDDisplayRecord *records, int count) {
  if (records == NULL) {
    return;
  }
  for (int i = 0; i < count; i++) {
    if (records[i].adapter != MACH_PORT_NULL) {
      IOObjectRelease(records[i].adapter);
      records[i].adapter = MACH_PORT_NULL;
    }
  }
}

// sd_open_transport 为某台显示器建立 DDC 通道，返回 IOAVServiceRef（调用方
// 负责 CFRelease）与 chip 地址。失败返回 NULL。
SD_IOAVServiceRef sd_open_transport(io_service_t adapter,
                                    uint32_t *chipAddress) {
  resolve_symbols();
  if (adapter == MACH_PORT_NULL || p_IOAVServiceCreateWithService == NULL) {
    return NULL;
  }
  uint64_t adapterID = 0;
  if (IORegistryEntryGetRegistryEntryID(adapter, &adapterID) != KERN_SUCCESS) {
    return NULL;
  }

  proxy_ctx ctx = {.targetAdapterID = adapterID, .matchedFramebuffer = false};
  io_service_t proxy = find_service(proxy_predicate, &ctx);
  if (proxy == MACH_PORT_NULL) {
    return NULL;
  }

  SD_IOAVServiceRef svc =
      p_IOAVServiceCreateWithService(kCFAllocatorDefault, proxy);

  Boolean mcdp = is_mcdp29xx(proxy);
  IOObjectRelease(proxy);

  if (svc == NULL) {
    return NULL;
  }
  if (chipAddress != NULL) {
    *chipAddress = mcdp ? SD_DDC_CHIP_ADDRESS_MCDP29XX
                        : SD_DDC_CHIP_ADDRESS_DEFAULT;
  }
  return svc;
}

// sd_default_service 是拿不到指定显示器时的兜底通道（等同于 m1ddc 的
// getDefaultDisplayAVService）。返回 NULL 表示不可用。
SD_IOAVServiceRef sd_default_service(void) {
  resolve_symbols();
  if (p_IOAVServiceCreate == NULL) {
    return NULL;
  }
  return p_IOAVServiceCreate(kCFAllocatorDefault);
}
