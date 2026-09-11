<script setup>
import { computed } from 'vue'
import { formatTime } from './format'
import { cardFor } from './registry'
import { isSystem, resolveKind } from './resolve'
import './cards.css'

const props = defineProps({
  msg: { type: Object, required: true },
})

const kind = computed(() => resolveKind(props.msg))
const sys = computed(() => isSystem(props.msg))
const card = computed(() => cardFor(kind.value))
</script>

<template>
  <div class="bubble-row" :class="{ send: msg.is_send, sys }">
    <div class="bubble" :class="['kind-' + kind, { sys }]">
      <component :is="card" :msg="msg" />
      <div v-if="!sys" class="bubble-time">{{ formatTime(msg.create_time) }}</div>
    </div>
  </div>
</template>
