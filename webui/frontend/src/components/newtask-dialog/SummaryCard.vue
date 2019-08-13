<template>
  <div class="card my-2 m-md-2 " :class="{'border-danger': !hasContent, 'border-success': hasContent}">
    <div class="card-header text-left">{{ cardTitle }}</div>
    <div
      class="card-body"
      :class="{
        'text-success': typeof(this.contentToEvaluate) == 'string',
        'font-weight-bold': typeof(this.contentToEvaluate) == 'string'
      }"
    >
      <slot v-if="hasContent" />
      <span v-else class="text-danger font-weight-bold">-</span>
    </div>
  </div>
</template>

<script>
export default {
  props: {
    cardTitle: {
      type: String,
      required: true
    },
    contentToEvaluate: {
      required: true
    }
  },
  computed: {
    hasContent () {
      if (typeof this.contentToEvaluate == 'string') {
        if (this.contentToEvaluate) return true
        else return false
      } else {
        // Array type
        if (this.contentToEvaluate.length > 0) return true
        else return false
      }
    }
  }
}
</script>