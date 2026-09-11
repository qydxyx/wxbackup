<script setup>
import { computed } from 'vue'
import { extraText } from './extra'

const props = defineProps({ msg: { type: Object, required: true } })
const amount = computed(() => extraText(props.msg, 'amount') || extraText(props.msg, 'title') || props.msg.text || '转账')
const memo = computed(() => extraText(props.msg, 'desc') || extraText(props.msg, 'status'))
</script>

<template>
  <div class="msg-card msg-pay msg-transfer">
    <div class="msg-pay-kicker">转账</div>
    <div class="msg-pay-title">{{ amount }}</div>
    <div v-if="memo" class="msg-pay-status">{{ memo }}</div>
  </div>
</template>
