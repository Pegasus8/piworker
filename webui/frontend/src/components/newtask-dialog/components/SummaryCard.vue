<template>
  <div class="card my-2 m-md-2" :class="{'border-danger': !hasContent, 'border-success': hasContent}">
    <div class="card-header text-left">
      {{ cardTitle }}
    </div>
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
    <div class="card-footer text-left" v-if="isList">
      <div class="custom-control custom-switch">
        <input type="checkbox" class="custom-control-input" :id="switchID" v-model="switchState">
        <label class="custom-control-label small text-muted" :for="switchID">Draggable</label>
      </div>
    </div>
  </div>
</template>

<script>
export default {
  data () {
    return {
      switchID: 'switch' + this._uid,
      switchState: true
    }
  },
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
    },
    isList () {
      if (typeof this.contentToEvaluate == 'string') return false
      else return true
    }
  },
  watch: {
    switchState (newState, oldState) {
      this.$emit('switchChange', newState)
    }
  }
}
</script>