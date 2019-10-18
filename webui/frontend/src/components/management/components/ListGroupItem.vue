<template>
  <li
    class="list-group-item bg-dark text-center"
    >
    <div>
      <div v-if="!editable" class="" id="nameOfItem">
        {{ itemName }}
      </div>
      <div class="row mt-1">
        <div class="text-right text-wrap col-10 text-center m-auto" style="font-size: 0.8em; max-width: 60%;">
          Args:
          <div class="list-group mt-1">
            <div class="list-group-item bg-dark text-muted text-left p-2" v-for="arg in args" :key="arg.ID + $uuid.v1()">
              <span class="h6" style="font-size: 1.2em;">{{ arg.Name }}: </span><span class="font-weight-bolder">{{ arg.Content }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </li>
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