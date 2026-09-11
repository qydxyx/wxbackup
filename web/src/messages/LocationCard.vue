<script setup>
import { computed } from 'vue'
import { extraText } from './extra'

const props = defineProps({ msg: { type: Object, required: true } })
const name = computed(() => extraText(props.msg, 'poiname') || extraText(props.msg, 'title') || '位置')
const address = computed(() => extraText(props.msg, 'address') || extraText(props.msg, 'label') || props.msg.text)
</script>

<template>
  <div class="msg-card msg-card-location">
    <div class="msg-card-kicker">位置</div>
    <div class="msg-card-title">{{ name }}</div>
    <div v-if="address" class="msg-card-desc">{{ address }}</div>
  </div>
</template>
