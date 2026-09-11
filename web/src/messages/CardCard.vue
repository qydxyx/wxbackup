<script setup>
import { computed } from 'vue'
import { extraText } from './extra'

const props = defineProps({ msg: { type: Object, required: true } })
const name = computed(() => extraText(props.msg, 'nickname') || extraText(props.msg, 'title') || props.msg.text || '名片')
const id = computed(() => extraText(props.msg, 'username') || extraText(props.msg, 'wxid'))
</script>

<template>
  <div class="msg-card msg-card-contact">
    <div class="msg-card-kicker">名片</div>
    <div class="msg-card-title">{{ name }}</div>
    <div v-if="id" class="msg-card-desc">{{ id }}</div>
  </div>
</template>
