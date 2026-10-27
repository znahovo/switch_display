<script setup lang="ts">
// 共享页：局域网内发现对端 → 选一台当主机 → 双方自动配对并启动 Deskflow。
//
// 交互设计的两个约定：
// 1) 「作为主机」只出现在**对端**卡片上——用户在本机点「用这台作为主机」，
//    意思是「我当主机，把你接进来」，所以按钮文案带对端名字，避免歧义。
// 2) 一旦本机在共享中，本页只留「停止共享」一个主动作，配对按钮全部禁用，
//    因为 Deskflow 一次只能跑一个角色。
import { computed, onMounted, ref } from "vue"
import { useI18n } from "vue-i18n"
import {
  ArrowRightLeft,
  Check,
  CircleDot,
  Copy,
  Cpu,
  Loader2,
  Monitor,
  Radio,
  RefreshCw,
  Square,
  Terminal,
  Unlink,
  Wifi,
} from "lucide-vue-next"
import PageShell from "@/components/PageShell.vue"
import Badge from "@/components/ui/Badge.vue"
import Button from "@/components/ui/Button.vue"
import Card from "@/components/ui/Card.vue"
import Input from "@/components/ui/Input.vue"
import SettingRow from "@/components/SettingRow.vue"
import { useLinkStore } from "@/stores/link"

const { t } = useI18n()
const store = useLinkStore()

const nameDraft = ref("")
const editingName = ref(false)
const showLog = ref(false)
const copied = ref(false)

onMounted(async () => {
  await store.refresh()
  nameDraft.value = store.self.name
})

const busy = computed(() => store.pairingId !== null)
/** lastHostId 是上次连过的主机，用来在列表里给它加个角标。 */
const lastHostId = computed(() => store.state.lastHostId ?? "")
/**
 * canPair 决定对端卡片上的两个按钮能不能点。
 *
 * 四个条件缺一不可：本机没在共享、装了 Deskflow、没有正在进行的配对、
 * 对端还在线。把它们收进一个函数，是为了「主机」和「加入」两个按钮
 * 永远同步启用/禁用——曾经分开判断，改一处忘一处就会让两个按钮状态不一致。
 */
function canPair(peer: { reachable: boolean }): boolean {
  return !store.running && store.state.deskflowAvailable && !busy.value && peer.reachable
}

/** roleText 把 idle/server/client 翻成界面文案。 */
function roleText(role: string): string {
  if (role === "server") return t("share.roleServer")
  if (role === "client") return t("share.roleClient")
  return t("share.roleIdle")
}

function roleVariant(role: string) {
  if (role === "server") return "success" as const
  if (role === "client") return "warning" as const
  return "outline" as const
}

/** shortFingerprint 只露头尾：完整 64 位 hex 在界面上没有可读性。 */
function shortFingerprint(fp: string): string {
  if (!fp) return "—"
  const body = fp.replace("v2:sha256:", "")
  if (body.length <= 16) return body
  return `${body.slice(0, 8)}…${body.slice(-8)}`
}

async function copyFingerprint() {
  if (!store.self.fingerprint) return
  try {
    await navigator.clipboard.writeText(store.self.fingerprint)
    copied.value = true
    setTimeout(() => (copied.value = false), 1600)
  } catch {
    // 剪贴板可能被系统拒绝，用户还能看到指纹头尾，不算致命。
  }
}

async function saveName() {
  const ok = await store.rename(nameDraft.value)
  if (ok) editingName.value = false
}

function cancelName() {
  nameDraft.value = store.self.name
  editingName.value = false
}

const coreStateText = computed(() => {
  const s = store.core.state
  if (s === "running") return t("share.coreRunning")
  if (s === "starting") return t("share.coreStarting")
  if (s === "failed") return t("share.coreFailed")
  return t("share.coreStopped")
})
</script>

<template>
  <PageShell :title="t('share.title')" :hint="t('share.hint')">
    <template #actions>
      <Button variant="outline" size="sm" :disabled="store.loading" @click="store.refresh()">
        <RefreshCw class="size-3.5" />
        {{ t("common.refresh") }}
      </Button>
      <Button v-if="store.running" variant="destructive" size="sm" @click="store.stop()">
        <Square class="size-3.5" />
        {{ t("share.stop") }}
      </Button>
    </template>

    <!-- Deskflow 没装：这是唯一会让人「点了没反应」的原因，先说清楚 -->
    <Card v-if="!store.state.deskflowAvailable" class="border-amber-500/35 bg-amber-500/[0.06] px-4 py-3">
      <div class="flex items-start gap-3">
        <Terminal class="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
        <div class="min-w-0 flex-1">
          <p class="text-[13px] font-medium">{{ t("share.noDeskflow") }}</p>
          <p class="mt-1 text-[12px] leading-relaxed text-muted-foreground">
            {{ t("share.noDeskflowHint") }}
          </p>
          <p class="mt-1.5 font-mono text-[11px] text-muted-foreground/80">
            {{ store.state.deskflowDir }}/deskflow.conf
          </p>
          <Button variant="outline" size="sm" class="mt-2.5" @click="store.rescanDeskflow()">
            <RefreshCw class="size-3.5" />
            {{ t("share.rescan") }}
          </Button>
        </div>
      </div>
    </Card>

    <!-- 本机身份 -->
    <Card class="px-4 py-3">
      <SettingRow :title="t('share.selfName')" :hint="t('share.selfNameHint')">
        <div v-if="editingName" class="flex items-center gap-1.5">
          <Input v-model="nameDraft" class="h-8 w-[168px] text-[13px]" @keyup.enter="saveName" />
          <Button size="sm" @click="saveName">{{ t("common.save") }}</Button>
          <Button variant="ghost" size="sm" @click="cancelName">{{ t("common.cancel") }}</Button>
        </div>
        <div v-else class="flex items-center gap-2">
          <span class="max-w-[200px] truncate text-[13px] font-medium">{{ store.self.name || "—" }}</span>
          <Button variant="ghost" size="sm" @click="editingName = true">
            {{ t("share.rename") }}
          </Button>
        </div>
      </SettingRow>

      <div class="mt-1 border-t border-border/60 pt-3">
        <SettingRow :title="t('share.role')" :hint="t('share.roleHint')">
          <Badge :variant="roleVariant(store.role)">
            <CircleDot class="size-3" />
            {{ roleText(store.role) }}
          </Badge>
        </SettingRow>
      </div>

      <div class="mt-1 border-t border-border/60 pt-3">
        <SettingRow :title="t('share.fingerprint')" :hint="t('share.fingerprintHint')">
          <div class="flex items-center gap-1.5">
            <span class="font-mono text-[12px] text-muted-foreground">
              {{ shortFingerprint(store.self.fingerprint) }}
            </span>
            <Button
              variant="ghost"
              size="icon"
              class="size-7"
              :disabled="!store.self.fingerprint"
              :title="t('share.copyFingerprint')"
              @click="copyFingerprint"
            >
              <Check v-if="copied" class="size-3.5 text-emerald-600" />
              <Copy v-else class="size-3.5" />
            </Button>
          </div>
        </SettingRow>
      </div>
    </Card>

    <!-- 发现到的设备 -->
    <section class="flex flex-col gap-2">
      <div class="flex items-center justify-between px-1">
        <h2 class="flex items-center gap-1.5 text-[13px] font-medium">
          <Radio class="size-3.5 text-muted-foreground" />
          {{ t("share.nearby") }}
        </h2>
        <span class="text-[11px] text-muted-foreground">
          {{ t("share.nearbyHint", { port: store.state.discoverPort }) }}
        </span>
      </div>

      <div v-if="store.peers.length" class="flex flex-col gap-2">
        <div
          v-for="peer in store.peers"
          :key="peer.id"
          class="flex items-center gap-3 rounded-xl border border-border bg-card px-4 py-3"
          :class="{ 'opacity-55': !peer.reachable }"
        >
          <div class="flex size-9 shrink-0 items-center justify-center rounded-lg bg-secondary">
            <Monitor class="size-4" />
          </div>

          <div class="min-w-0 flex-1">
            <div class="flex items-center gap-1.5">
              <span class="truncate text-[13px] font-medium leading-5">{{ peer.name }}</span>
              <Badge v-if="peer.paired" variant="success">
                <Check class="size-3" />
                {{ t("share.paired") }}
              </Badge>
              <Badge v-if="peer.id === lastHostId && !peer.paired" variant="outline">
                {{ t("share.lastHost") }}
              </Badge>
              <Badge v-if="!peer.reachable" variant="outline">{{ t("share.offline") }}</Badge>
            </div>
            <p class="truncate font-mono text-[11px] leading-4 text-muted-foreground">
              {{ peer.address }} · {{ roleText(peer.role) }}
            </p>
          </div>

          <div class="flex shrink-0 items-center gap-1.5">
            <Button
              variant="outline"
              size="sm"
              :disabled="!canPair(peer)"
              :title="t('share.hostWithHint', { name: peer.name })"
              @click="store.hostWith(peer)"
            >
              <Loader2 v-if="store.pairingId === peer.id" class="size-3.5 animate-spin" />
              <Cpu v-else class="size-3.5" />
              {{ t("share.hostWith") }}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              :disabled="!canPair(peer)"
              :title="t('share.joinHint', { name: peer.name })"
              @click="store.joinHost(peer)"
            >
              <ArrowRightLeft class="size-3.5" />
              {{ t("share.join") }}
            </Button>
          </div>
        </div>
      </div>

      <Card v-else class="flex flex-col items-center gap-2 px-6 py-10 text-center">
        <Wifi class="size-5 text-muted-foreground" />
        <p class="text-[13px] text-muted-foreground">{{ t("share.noPeers") }}</p>
        <p class="text-[11px] text-muted-foreground/75">{{ t("share.noPeersHint") }}</p>
      </Card>
    </section>

    <!-- 已配对设备 -->
    <section v-if="store.trusted.length" class="flex flex-col gap-2">
      <h2 class="flex items-center gap-1.5 px-1 text-[13px] font-medium">
        <Check class="size-3.5 text-muted-foreground" />
        {{ t("share.trusted") }}
      </h2>
      <div class="flex flex-col gap-2">
        <div
          v-for="peer in store.trusted"
          :key="peer.id"
          class="flex items-center gap-3 rounded-xl border border-border/70 bg-card/60 px-4 py-2.5"
        >
          <div class="min-w-0 flex-1">
            <p class="truncate text-[13px]">{{ peer.name || peer.id.slice(0, 8) }}</p>
            <p v-if="peer.pairedAt" class="text-[11px] text-muted-foreground">
              {{ t("share.pairedAt", { time: peer.pairedAt }) }}
            </p>
          </div>
          <Button variant="ghost" size="sm" class="shrink-0" @click="store.unpair(peer.id)">
            <Unlink class="size-3.5" />
            {{ t("share.unpair") }}
          </Button>
        </div>
      </div>
    </section>

    <!-- core 状态 -->
    <Card class="px-4 py-3">
      <SettingRow :title="t('share.core')" :hint="store.coreMsgText(t) || t('share.coreHint')">
        <div class="flex items-center gap-2">
          <Badge :variant="store.running ? 'success' : store.core.state === 'failed' ? 'danger' : 'outline'">
            {{ coreStateText }}
          </Badge>
          <span v-if="store.core.pid" class="font-mono text-[11px] text-muted-foreground">
            PID {{ store.core.pid }}
          </span>
          <Button
            v-if="store.core.logTail?.length"
            variant="ghost"
            size="sm"
            @click="showLog = !showLog"
          >
            {{ showLog ? t("share.hideLog") : t("share.showLog") }}
          </Button>
        </div>
      </SettingRow>
      <pre
        v-if="showLog && store.core.logTail?.length"
        class="mt-2 max-h-52 overflow-auto rounded-lg bg-muted/50 p-2.5 font-mono text-[11px] leading-relaxed"
        >{{ store.core.logTail.join("\n") }}</pre
      >
      <p class="mt-2 truncate font-mono text-[11px] text-muted-foreground/75">
        {{ store.state.deskflowBinary || store.state.deskflowDir }}
      </p>
      <!-- GPL-2.0 的署名要求：内置分发了 Deskflow 的二进制，必须说明来源。 -->
      <p class="mt-1 text-[11px] leading-relaxed text-muted-foreground/70">
        {{ t("share.license") }}
      </p>
    </Card>

    <p v-if="store.noticeText(t)" class="text-[12px] text-muted-foreground">
      {{ store.noticeText(t) }}
    </p>
    <p v-if="store.notice === 'not-found'" class="text-[12px] text-amber-600 dark:text-amber-400">
      {{ t("share.stillNotFound") }}
    </p>
    <p v-if="store.stateErrorText(t)" class="text-[13px] text-destructive">
      {{ store.stateErrorText(t) }}
    </p>
    <p v-if="store.error" class="text-[13px] text-destructive">{{ store.errorText(t) }}</p>
  </PageShell>
</template>
