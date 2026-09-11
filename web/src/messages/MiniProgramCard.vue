<script setup>
import { computed } from 'vue'
import { extraText } from './extra'

const props = defineProps({ msg: { type: Object, required: true } })
const name = computed(() => extraText(props.msg, 'name') || extraText(props.msg, 'app_name'))
const title = computed(() => extraText(props.msg, 'title') || props.msg.text || '小程序')
</script>

<template>
  <div class="msg-card msg-card-miniprogram">
    <div class="msg-card-kicker">小程序{{ name ? ` · ${name}` : '' }}</div>
    <div class="msg-card-title">{{ title }}</div>
  </div>
</template>
