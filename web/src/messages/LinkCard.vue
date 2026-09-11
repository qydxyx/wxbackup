<script setup>
import { computed } from 'vue'
import { extraText } from './extra'

const props = defineProps({ msg: { type: Object, required: true } })
const title = computed(() => extraText(props.msg, 'title') || props.msg.text || '链接')
const desc = computed(() => extraText(props.msg, 'desc'))
const href = computed(() => extraText(props.msg, 'url'))
</script>

<template>
  <div class="msg-card msg-card-link">
    <div class="msg-card-kicker">链接</div>
    <a v-if="href" class="msg-card-title" :href="href" target="_blank" rel="noreferrer">{{ title }}</a>
    <div v-else class="msg-card-title">{{ title }}</div>
    <div v-if="desc" class="msg-card-desc">{{ desc }}</div>
  </div>
</template>
