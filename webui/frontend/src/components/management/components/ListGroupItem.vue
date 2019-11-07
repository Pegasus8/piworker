<template>
  <b-list-group-item
    class="bg-dark text-center"
    >
    <div>
      <div v-if="!editable" class="" id="nameOfItem">
        {{ itemName }}
      </div>
      <b-row class="mt-1">
        <div class="text-right text-wrap col-10 text-center m-auto" style="font-size: 0.8em; max-width: 60%;">
          Args:
          <b-list-group variant="primary" class="mt-1">
            <b-list-group-item class="text-muted text-left p-2" v-for="arg in args" :key="arg.ID + $uuid.v1()">
              <span class="h6" style="font-size: 1.2em;">{{ arg.Name }}: </span><span class="font-weight-bolder">{{ arg.Content }}</span>
            </b-list-group-item>
          </b-list-group>
        </div>
      </b-row>
    </div>
  </b-list-group-item>
</template>

<script>
export default {
  props: {
    editable: {
      type: Boolean,
      required: false,
      default: false
    },
    itemName: {
      type: String,
      required: true
    },
    args: {
      type: Array,
      required: true
    }
  },
  computed: {
  }
}
</script>

<style lang="scss" scoped>
</style>