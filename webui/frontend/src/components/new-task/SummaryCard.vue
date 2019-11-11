<template>
  <b-card no-body class="my-2 m-md-2" :class="{'border-danger': !hasContent, 'border-success': hasContent}">
    <b-card-header class="text-left">
      {{ cardTitle }}
    </b-card-header>
    <b-card-body
      :class="{
        'text-success': typeof(this.contentToEvaluate) == 'string',
        'font-weight-bold': typeof(this.contentToEvaluate) == 'string'
      }"
    >
      <slot v-if="hasContent" />
      <span v-else class="text-danger font-weight-bold">-</span>
    </b-card-body>
    <b-card-footer class="text-left" v-if="isList">
      <b-form-checkbox
        :id="switchID"
        v-model="switchState"
      >
        <label class="small text-muted">Draggable</label>
      </b-form-checkbox>
    </b-card-footer>
  </b-card>
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
      if (typeof this.contentToEvaluate === 'string') {
        if (this.contentToEvaluate) return true
        else return false
      } else {
        // Array type
        if (this.contentToEvaluate.length > 0) return true
        else return false
      }
    },
    isList () {
      if (typeof this.contentToEvaluate === 'string') return false
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
