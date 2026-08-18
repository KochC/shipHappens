<script setup>
import { ref, computed } from 'vue'

const count = ref(0)
const doubled = computed(() => count.value * 2)

function increment() {
  count.value++
}
</script>

<template>
  <div class="counter">
    <p class="count">count: {{ count }}</p>
    <p class="doubled">doubled: {{ doubled }}</p>
    <button type="button" @click="increment">increment</button>
  </div>
</template>
