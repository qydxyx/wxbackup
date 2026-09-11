<script setup>
import { computed } from 'vue'
import { extraText } from './extra'

const props = defineProps({ msg: { type: Object, required: true } })
const title = computed(() => extraText(props.msg, 'title') || props.msg.text || '视频号直播')
const author = computed(() => extraText(props.msg, 'nickname') || extraText(props.msg, 'name'))
</script>

<template>
  <div class="msg-card msg-card-live">
    <div class="msg-card-kicker">视频号直播{{ author ? ` · ${author}` : '' }}</div>
    <div class="msg-card-title">{{ title }}</div>
  </div>
</template>
