<script setup>
import { computed } from 'vue'
import { extraText } from './extra'

const props = defineProps({ msg: { type: Object, required: true } })
const title = computed(() => extraText(props.msg, 'title') || '群公告')
const body = computed(() => extraText(props.msg, 'desc') || props.msg.text)
</script>

<template>
  <div class="msg-card msg-card-groupnotice">
    <div class="msg-card-kicker">群公告</div>
    <div class="msg-card-title">{{ title }}</div>
    <div v-if="body" class="msg-card-desc">{{ body }}</div>
  </div>
</template>
