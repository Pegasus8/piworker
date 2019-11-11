<template>
  <div class="form-group card p-0 bg-dark">
    <div class="card-body">
      <label :for="topElementID" class="card-title text-light">{{ containerTitle }}</label>
      <slot name="top" />
      <small
        :id="topElementID + '-description'"
        class="form-text text-muted text-left"
      >{{ containerDescription }}</small>
    </div>
    <div v-if="withFooter" class="card-footer">
      <slot name="bottom" />
    </div>
  </div>
</template>

<script>
export default {
  props: {
    containerTitle: {
      type: String,
      required: true,
      default: ''
    },
    containerDescription: {
      type: String,
      required: true,
      default: ''
    },
    topElementID: {
      type: String,
      required: true,
      default: ''
    },
    withFooter: {
      type: Boolean,
      required: false,
      default: false
    }
  },
  data () {
    return {
      hasBottomSlot: false
    }
  }
}
</script>
