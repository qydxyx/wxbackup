<script setup>
import { computed } from 'vue'
import { resolveInfo } from './resolve'

const props = defineProps({ msg: { type: Object, required: true } })
const info = computed(() => resolveInfo(props.msg))
</script>

<template>
  <div class="msg-card msg-card-placeholder">
    <div class="msg-card-kicker">暂不支持</div>
    <div class="msg-card-title">{{ info.label }}</div>
    <div v-if="msg.text" class="msg-card-desc">{{ msg.text }}</div>
  </div>
</template>
